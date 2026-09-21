"""Phase 4：把模型吐出來的原始異常分數，換算成「贏過基準母體多少百分比」。

不重訓——直接載入已經 fit 好的 model.joblib，把訓練時用的同一批基準地址
（crawler/train-data）重新跑一次算分，取幾個關鍵百分位數的分數門檻存進
manifest.json。上線那邊（scoring/service.py）用這幾個門檻做內插，把一個
原始浮點數換算成「這個分數落在基準母體的第幾百分位」，人才看得懂。

    python -m scoring.train.percentiles --data ../crawler/train-data

門檻點包含 90——驗收報告（train.py 的 signal()）量過的就是「基準前 10%」
這條線，其餘幾點只是讓內插更平滑，不是另外驗證過的门檻。
"""

import argparse
import json
import pathlib

import joblib
import numpy as np

from scoring.train import dataset
from scoring.train.train import anomaly

PERCENTILE_POINTS = (5, 10, 25, 50, 75, 90, 95, 99)


def main() -> None:
    parser = argparse.ArgumentParser(description="算基準母體的百分位數門檻，寫進 manifest.json")
    parser.add_argument(
        "--data", type=pathlib.Path, required=True,
        help="基準地址資料夾——必須是訓練這個 model.joblib 時用的同一批",
    )
    parser.add_argument(
        "--model", type=pathlib.Path, default=pathlib.Path("scoring/model"),
        help="model.joblib 跟 manifest.json 所在目錄",
    )
    arguments = parser.parse_args()

    artifact = joblib.load(arguments.model / "model.joblib")
    scaler, model, keep = artifact["scaler"], artifact["model"], artifact["keep"]

    print("讀基準資料…", flush=True)
    base_addresses, base_rows = dataset.load(arguments.data)
    print(f"  {len(base_addresses)} 個地址")

    scores = anomaly(scaler, model, keep, base_rows)
    cutoffs = {str(p): float(np.percentile(scores, p)) for p in PERCENTILE_POINTS}

    manifest_path = arguments.model / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    manifest["baselinePercentiles"] = cutoffs
    manifest_path.write_text(
        json.dumps(manifest, ensure_ascii=False, indent=1), encoding="utf-8"
    )

    print("\n百分位數門檻（分數 -> 排名）：")
    for p in PERCENTILE_POINTS:
        print(f"  {p:3d}%  {cutoffs[str(p)]:+.4f}")
    print(f"\n寫進 {manifest_path}")


if __name__ == "__main__":
    main()
