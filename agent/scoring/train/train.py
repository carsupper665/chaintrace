"""訓練異常偵測模型，然後想辦法證明它是錯的。

    python -m scoring.train.train --data ../crawler/train-data --labels ../crawler/label-data

模型本身十行就寫完。難的是第二件事：一個無監督模型永遠會吐出分數，看起來永遠
很合理，所以唯一能分辨「抓到可疑」和「抓到錢多」的方法，是拿已知答案去對。

兩個指標，否決權比訊號大：

  訊號  被 Tether 凍結的地址應該明顯集中在異常排名前段（隨機是 10%）。用凍結
        而不是 OFAC 制裁名單：實測 178 個制裁地址裡只有極少數在窗口內有 USDT
        活動，它們被制裁後資金就凍住了，拿來測「行為偵測」等於在測「有沒有
        活動」。被 Tether 凍結的地址是因為當下的違法行為才被凍的，凍結前必然
        活躍，才是這個模型該抓的東西。
  否決  已知的交易所不能佔滿最前面。佔滿了就表示模型學到的是規模不是行為，
        這時候要回去改特徵，調閾值只是把結果藏起來。

前處理用 QuantileTransformer，不是任何線性縮放。實測的差別大到不能忽略：改用
RobustScaler 時，異常排名前 20 名裡有 12 個是已知交易所熱錢包；換成分位數轉換
後是 0 個，交易所散落在第 83 到 277 名。

原因是線性縮放不改變倍率關係 —— 一個交易所在「轉帳筆數」上超出母體五十倍，
縮放完還是五十倍，Isolation Forest 只要靠規模就能把它隔離出來。分位數轉換把每
個特徵換成它在母體中的排名，最大值只是「分佈頂端」，有界，模型就必須看行為的
形狀而不是大小。
"""

import argparse
import hashlib
import json
import pathlib
import sys

import numpy as np
from sklearn.ensemble import IsolationForest
from sklearn.preprocessing import QuantileTransformer

from scoring.features import FEATURE_NAMES
from scoring.train import dataset

RANDOM_STATE = 20260902
# 驗收門檻，來自 docs/learned-risk-scoring-plan.md：壞地址落在前 10% 的比例
# 要達到隨機（10%）的三倍左右。這個數字不准為了讓結果好看而調低。
SIGNAL_THRESHOLD = 30.0
# 兩個特徵相關到這個程度就是同一件事講兩次，模型會重複加權它。
MAX_CORRELATION = 0.9
# 我在原始候選清單外加的特徵，消融測試逼它證明自己值得。
# 另一項「帳戶年齡」已經因為沒通過這個檢驗而被拿掉（見 features.py）。
ABLATED = ("flow_match_share",)


def fit(rows, columns=None):
    """回傳 (scaler, model, 用到的欄位索引)。columns 為 None 就用全部。"""
    matrix = np.asarray(rows, dtype=float)
    keep = list(range(matrix.shape[1])) if columns is None else columns
    scaler = QuantileTransformer(
        output_distribution="normal",
        n_quantiles=min(500, len(matrix)),
        random_state=RANDOM_STATE,
    ).fit(matrix[:, keep])
    model = IsolationForest(
        n_estimators=300, random_state=RANDOM_STATE, contamination="auto"
    ).fit(scaler.transform(matrix[:, keep]))
    return scaler, model, keep


def anomaly(scaler, model, keep, rows):
    """越大越異常。sklearn 的 score_samples 是反過來的，這裡轉正。"""
    matrix = np.asarray(rows, dtype=float)[:, keep]
    return -model.score_samples(scaler.transform(matrix))


def signal(baseline_scores, target_scores) -> float:
    """目標分數落在基準分佈前 10% 的比例。隨機的話會是 0.10。"""
    threshold = np.quantile(baseline_scores, 0.90)
    return float(np.mean(target_scores >= threshold)) if len(target_scores) else 0.0


def correlated_pairs(rows) -> list[tuple[float, str, str]]:
    """相關性超標的特徵對。開訓前一定要看：兩個講同一件事的特徵會讓那件事
    拿到雙倍權重，模型就開始照規模排名而不是照行為。"""
    matrix = np.corrcoef(np.asarray(rows, dtype=float), rowvar=False)
    return sorted(
        (
            (abs(matrix[i, j]), FEATURE_NAMES[i], FEATURE_NAMES[j])
            for i in range(len(FEATURE_NAMES))
            for j in range(i + 1, len(FEATURE_NAMES))
            if abs(matrix[i, j]) > MAX_CORRELATION
        ),
        reverse=True,
    )


def report(title, addresses, scores, labels, baseline_scores):
    kinds = {}
    for address, score in zip(addresses, scores):
        kinds.setdefault(labels.get(address, "unlabelled"), []).append(score)
    print(f"\n{title}")
    for kind, values in sorted(kinds.items()):
        print(
            f"  {kind:12} n={len(values):4d}  中位數 {np.median(values):+.4f}"
            f"  落在基準前 10% 的比例 {signal(baseline_scores, np.array(values)) * 100:5.1f}%"
        )


def main() -> None:
    parser = argparse.ArgumentParser(description="訓練並驗證學習式風險評分")
    parser.add_argument("--data", type=pathlib.Path, required=True, help="訓練資料夾")
    parser.add_argument("--labels", type=pathlib.Path, required=True, help="標籤資料夾")
    parser.add_argument("--out", type=pathlib.Path, default=pathlib.Path("scoring/model"))
    arguments = parser.parse_args()

    print("讀訓練資料…", flush=True)
    base_addresses, base_rows = dataset.load(arguments.data)
    print(f"  {len(base_addresses)} 個地址")

    print("讀標籤資料…", flush=True)
    label_addresses, label_rows = dataset.load(arguments.labels)
    labels = json.loads((arguments.labels / "labels.json").read_text(encoding="utf-8"))
    labels = {a: ("exchange" if v.startswith("exchange") else v) for a, v in labels.items()}
    print(f"  {len(label_addresses)} 個地址")

    overlapping = correlated_pairs(base_rows)
    if overlapping:
        print(f"\n相關性超過 {MAX_CORRELATION} 的特徵對，要砍掉一邊：")
        for value, left, right in overlapping:
            print(f"  {value:.3f}  {left} / {right}")
        sys.exit(1)
    print(f"相關性檢查通過：沒有任何一對超過 {MAX_CORRELATION}")

    # 基準只用抽樣來的一般地址訓練。把標籤地址也餵進去，模型就會把它們當成
    # 「正常」的一部分，等於先偷看答案。
    scaler, model, keep = fit(base_rows)
    base_scores = anomaly(scaler, model, keep, base_rows)
    label_scores = anomaly(scaler, model, keep, label_rows)

    report("各類別的異常分數", label_addresses, label_scores, labels, base_scores)

    def scores_for(kind):
        return np.array(
            [s for a, s in zip(label_addresses, label_scores) if labels.get(a) == kind]
        )

    print("\n" + "=" * 62)
    # 主要指標用「被 Tether 凍結」而不是「被 OFAC 制裁」：實測 178 個制裁地址
    # 裡只有 3 個在窗口內有 USDT 活動，被制裁後資金就凍住了。它們的高分只證明
    # 模型會把「沒有活動」判成異常，那是另一個問題，不是行為偵測。
    hit = signal(base_scores, scores_for("blacklisted")) * 100
    print(f"訊號  被凍結地址落在前 10%：{hit:.1f}%   （隨機 10%，目標 ~30%）")
    print(
        f"      （參考：被制裁地址 {signal(base_scores, scores_for('sanctioned')) * 100:.1f}%，"
        "但它們 98% 休眠，這個數字只反映「沒有活動也算異常」）"
    )

    pool = list(zip(base_addresses, base_scores)) + list(zip(label_addresses, label_scores))
    pool.sort(key=lambda item: item[1], reverse=True)
    top = pool[: max(len(pool) // 100, 1)]
    exchanges_on_top = sum(1 for a, _ in top if labels.get(a) == "exchange")
    print(
        f"否決  前 1%（{len(top)} 個）裡的交易所：{exchanges_on_top} 個"
        f"（{exchanges_on_top / len(top) * 100:.0f}%，必須不過半）"
    )
    print("=" * 62)
    print("\n異常排名最前面 10 個：")
    for address, score in pool[:10]:
        print(f"  {score:+.4f}  {address}  {labels.get(address, '—')}")

    print("\n消融測試（拿掉我自己加的特徵，看訊號掉多少）：")
    columns = [i for i, n in enumerate(FEATURE_NAMES) if n not in ABLATED]
    a_scaler, a_model, a_keep = fit(base_rows, columns)
    a_base = anomaly(a_scaler, a_model, a_keep, base_rows)
    a_hit = signal(a_base, anomaly(a_scaler, a_model, a_keep, [
        r for a, r in zip(label_addresses, label_rows) if labels.get(a) == "blacklisted"
    ])) * 100
    print(f"  少了 {', '.join(ABLATED)}：{a_hit:.1f}%（完整版 {hit:.1f}%）")

    passed = hit >= SIGNAL_THRESHOLD and exchanges_on_top <= len(top) / 2
    if not passed:
        print("\n>>> Phase 2 驗收未過。回頭改特徵，不要調閾值。")
        sys.exit(1)

    arguments.out.mkdir(parents=True, exist_ok=True)
    import joblib

    # 特徵名稱跟模型存在一起。只存模型的話，日後特徵順序一改，線上會拿
    # 「轉帳筆數」當「金額中位數」算，而且照樣吐出一個看起來正常的分數。
    joblib.dump(
        {"scaler": scaler, "model": model, "features": list(FEATURE_NAMES), "keep": keep},
        arguments.out / "model.joblib",
    )
    (arguments.out / "manifest.json").write_text(
        json.dumps(
            {
                **dataset.describe(arguments.data),
                "trainedOn": len(base_addresses),
                "trainingDataHash": hashlib.sha256(
                    (arguments.data / "transfers.jsonl.gz").read_bytes()
                ).hexdigest(),
                "blacklistedTop10Percent": round(hit, 1),
                "exchangesInTop1Percent": exchanges_on_top,
                "randomState": RANDOM_STATE,
                "libraries": {
                    "numpy": np.__version__,
                    "sklearn": __import__("sklearn").__version__,
                },
            },
            ensure_ascii=False,
            indent=1,
        ),
        encoding="utf-8",
    )
    print(f"\n>>> 驗收通過。模型存到 {arguments.out}")


if __name__ == "__main__":
    main()
