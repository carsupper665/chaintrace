"""訓練學習式風險評分：兩種方法都訓練，用「舊窗口訓練、新窗口驗證」比較，留下比較好的。

    python -m scoring.train.train --windows ../crawler/windows \\
        --freezes ../crawler/freezes.json --unfreezes ../crawler/unfreezes.json

資料：crawler/windows/<窗口結束日>/{control,positives}，每個窗口 30 天（見 crawler/README.md）。
  可疑樣本  窗口結束後 LABEL_HORIZON_DAYS 天內被 Tether 凍結、之後沒被解凍的地址。
  未標記    窗口內隨機時間點抽到的一般地址（crawl.py --spread）。不是「確定正常」。
窗口內就已經被凍結的地址兩邊都不算。所有地址都只取前 TRANSFER_CAP 筆，跟線上
Go 抓的一樣（analysis/learned_score.go 的 LearnedScoreTransferCap）。

驗證不用隨機切分：每次用比較舊的窗口訓練，測試下一個窗口，跟上線後「用過去的
資料判斷現在的地址」是同一件事。測試窗口裡出現在訓練窗口的地址剔除。

兩道否決，都在存模型之前：

  抽樣偏差  只用「最後一筆交易距離窗口結束多久」預測，各測試窗口的 ROC-AUC
           平均必須在 0.5 ± RECENCY_TOLERANCE。從窗口結束往回掃抽樣的對照組
           100% 在最後一小時內活躍，光這個數字就能拿到 0.998 —— 模型學到的會是
           「抽樣時剛好活躍」，不是「可疑」。
  訊號     勝出方法的平均 ROC-AUC 要達到 SIGNAL_THRESHOLD。不准為了好看調低。

為什麼不再用 Isolation Forest：它找的是「不尋常」，不是「可疑」。在公平的對照組
上它的 ROC-AUC 是 0.536，接近亂猜（docs/adr/0018）。
"""

import argparse
import hashlib
import json
import pathlib
import sys
from datetime import timedelta

import joblib
import numpy as np
from sklearn.metrics import average_precision_score, roc_auc_score

from scoring.features import FEATURE_NAMES, compute, vector
from scoring.learned import RANDOM_STATE, SCORERS
from scoring.train import dataset

TRANSFER_CAP = 1000
WINDOW_DAYS = 30
LABEL_HORIZON_DAYS = 60
SIGNAL_THRESHOLD = 0.75
RECENCY_TOLERANCE = 0.1
# 最終模型訓練時留下這比例的未標記地址不用，拿來算百分位數門檻（模型沒看過它們）。
PERCENTILE_HOLDOUT = 0.2
PERCENTILE_POINTS = (5, 10, 25, 50, 75, 90, 95, 99)


def load_window(directory: pathlib.Path, freezes: dict, unfrozen: set) -> dict:
    """{地址: (特徵向量, 是否可疑, 最後一筆交易距窗口結束幾小時)}。"""
    rows = {}
    for part in ("control", "positives"):
        source = directory / part
        start, end = dataset.window(source)
        days = (end - start).days
        if days != WINDOW_DAYS:
            sys.exit(f"{source} 的窗口是 {days} 天，模型要求 {WINDOW_DAYS} 天")
        end_ms = end.timestamp() * 1000
        horizon_ms = end_ms + timedelta(days=LABEL_HORIZON_DAYS).total_seconds() * 1000
        for address, transfers, truncated in dataset.histories(source, TRANSFER_CAP):
            own = [t for t in transfers if address in (t.from_address, t.to_address)]
            frozen_at = freezes.get(address, 0)
            if not own or 0 < frozen_at <= end_ms or address in unfrozen:
                continue
            features = compute(transfers, address, window_start=start, window_end=end, truncated=truncated)
            hours = (end - max(t.timestamp for t in own)).total_seconds() / 3600
            rows[address] = (vector(features), end_ms < frozen_at <= horizon_ms, hours)
    return rows


def split(rows: dict):
    features = np.array([v[0] for v in rows.values()])
    labels = np.array([v[1] for v in rows.values()])
    return features[labels], features[~labels]


def evaluate(windows: list, data: dict) -> dict:
    """每個窗口（從第三個起）都用它之前的窗口訓練、拿它測試。"""
    results = {scorer.kind: [] for scorer in SCORERS}
    results["recency"] = []
    for i in range(2, len(windows)):
        train = {}
        for name in windows[:i]:
            train.update(data[name])
        test = {a: v for a, v in data[windows[i]].items() if a not in train}
        suspicious, unlabeled = split(train)
        rows = np.array([v[0] for v in test.values()])
        labels = np.array([v[1] for v in test.values()])
        recency = roc_auc_score(labels, -np.array([v[2] for v in test.values()]))
        results["recency"].append(recency)
        line = f"  {', '.join(windows[:i])} -> {windows[i]}  ({labels.sum()} 可疑 / {len(labels)})  最後交易時間 {recency:.3f}"
        for scorer in SCORERS:
            scores = scorer().fit(suspicious, unlabeled).score(rows)
            roc, pr = roc_auc_score(labels, scores), average_precision_score(labels, scores)
            results[scorer.kind].append((roc, pr))
            line += f"  {scorer.kind} {roc:.3f}/{pr:.3f}"
        print(line, flush=True)
    return results


def data_hash(paths) -> str:
    digest = hashlib.sha256()
    for path in paths:
        digest.update(hashlib.sha256(path.read_bytes()).digest())
    return digest.hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser(description="訓練並比較兩種學習式風險評分")
    parser.add_argument("--windows", type=pathlib.Path, required=True)
    parser.add_argument("--freezes", type=pathlib.Path, required=True, help="crawler/freezes.py 的輸出")
    parser.add_argument("--unfreezes", type=pathlib.Path, required=True, help="被解凍的地址，排除在可疑樣本外")
    parser.add_argument("--out", type=pathlib.Path, default=pathlib.Path("scoring/model"))
    arguments = parser.parse_args()

    freezes = json.loads(arguments.freezes.read_text(encoding="utf-8"))
    unfrozen = set(json.loads(arguments.unfreezes.read_text(encoding="utf-8")))
    windows = sorted(
        d.name for d in arguments.windows.iterdir()
        if (d / "control" / "transfers.jsonl.gz").exists() and (d / "positives" / "transfers.jsonl.gz").exists()
    )
    if len(windows) < 3:
        sys.exit(f"至少要 3 個窗口才能做時間外推驗證，只找到 {windows}")

    print("讀資料…", flush=True)
    data = {}
    for name in windows:
        data[name] = load_window(arguments.windows / name, freezes, unfrozen)
        suspicious = sum(v[1] for v in data[name].values())
        print(f"  {name}: {suspicious} 可疑, {len(data[name]) - suspicious} 未標記", flush=True)

    print("\n舊窗口訓練 → 新窗口驗證（ROC-AUC/PR-AUC）", flush=True)
    results = evaluate(windows, data)
    recency = float(np.mean(results["recency"]))
    summary = {
        scorer.kind: {
            "rocAuc": round(float(np.mean([r[0] for r in results[scorer.kind]])), 3),
            "prAuc": round(float(np.mean([r[1] for r in results[scorer.kind]])), 3),
        }
        for scorer in SCORERS
    }
    winner = max(SCORERS, key=lambda scorer: summary[scorer.kind]["rocAuc"])

    print("\n" + "=" * 62)
    print(f"抽樣偏差  只用最後交易時間：平均 ROC-AUC {recency:.3f}（必須在 0.5 ± {RECENCY_TOLERANCE}）")
    for kind, values in summary.items():
        print(f"{kind:9} 平均 ROC-AUC {values['rocAuc']:.3f}  PR-AUC {values['prAuc']:.3f}")
    print(f"勝出      {winner.kind}（門檻 ROC-AUC {SIGNAL_THRESHOLD}）")
    print("=" * 62)
    if abs(recency - 0.5) > RECENCY_TOLERANCE or summary[winner.kind]["rocAuc"] < SIGNAL_THRESHOLD:
        print("\n>>> 驗收未過。回頭查資料與特徵，不要調門檻。")
        sys.exit(1)

    # 最終模型用全部窗口訓練，但留一部分未標記地址不給它看，拿來算百分位數：
    # 拿訓練過的資料算，門檻會被壓低，線上每個地址看起來都比一般地址可疑。
    everything = {}
    for name in windows:
        everything.update(data[name])
    suspicious, unlabeled = split(everything)
    rng = np.random.default_rng(RANDOM_STATE)
    held = rng.random(len(unlabeled)) < PERCENTILE_HOLDOUT
    model = winner().fit(suspicious, unlabeled[~held])
    baseline = model.score(unlabeled[held])
    percentiles = {str(p): float(np.percentile(baseline, p)) for p in PERCENTILE_POINTS}

    arguments.out.mkdir(parents=True, exist_ok=True)
    # 特徵名稱跟模型存在一起。只存模型的話，日後特徵順序一改，線上會拿
    # 「轉帳筆數」當「金額中位數」算，而且照樣吐出一個看起來正常的分數。
    joblib.dump({"model": model, "features": list(FEATURE_NAMES)}, arguments.out / "model.joblib")
    files = [arguments.freezes, arguments.unfreezes] + [
        arguments.windows / name / part / "transfers.jsonl.gz" for name in windows for part in ("control", "positives")
    ]
    (arguments.out / "manifest.json").write_text(
        json.dumps(
            {
                "kind": winner.kind,
                "windowDays": WINDOW_DAYS,
                "transferCap": TRANSFER_CAP,
                "labelHorizonDays": LABEL_HORIZON_DAYS,
                "featureNames": list(FEATURE_NAMES),
                "windows": windows,
                "trainedOn": {"suspicious": int(len(suspicious)), "unlabeled": int((~held).sum())},
                "temporalValidation": summary,
                "recencyOnlyRocAuc": round(recency, 3),
                "trainingDataHash": data_hash(files),
                "baselinePercentiles": percentiles,
                "randomState": RANDOM_STATE,
                "libraries": {"numpy": np.__version__, "sklearn": __import__("sklearn").__version__},
            },
            ensure_ascii=False,
            indent=1,
        ),
        encoding="utf-8",
    )
    print(f"\n>>> 驗收通過，採用 {winner.kind}。模型存到 {arguments.out}")


if __name__ == "__main__":
    main()
