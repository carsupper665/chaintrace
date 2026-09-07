"""把爬回來的原始轉帳讀成 Address Feature Vector 矩陣。

這裡是 ADR-0015 的特徵一致性落地的地方：訓練用的矩陣，每一列都是 features.py
算出來的，跟線上打分走同一份程式碼、同一個窗口、同一個轉帳上限。窗口與上限從
資料自己的 manifest 讀出來，不是寫死在這裡 —— 換一批資料就換一組設定，模型也
就必須跟著重訓。

解壓的部分跟 crawler/crawl.py 長得很像，但兩邊不共用：crawler 是獨立部署單元，
複製到別台機器就要能跑，不能反過來依賴 agent。這十幾行的重複是那個邊界的代價，
不是疏忽。
"""

import json
import pathlib
import zlib
from datetime import datetime, timezone

from scoring.features import FEATURE_NAMES, Transfer, compute, vector

GZIP_MEMBER_MAGIC = b"\x1f\x8b\x08"


def read_records(path: pathlib.Path):
    """一行一筆地吐出資料檔的內容，跳過壞掉或半截的段落。

    append 模式下每次執行都是一個獨立的 gzip 段。只要有一段被中斷，gzip 模組
    讀到它就整個放棄 —— 實測有個 24 MB 的檔案因此被判成空的。
    """
    raw = path.read_bytes()
    offset = 0
    while True:
        offset = raw.find(GZIP_MEMBER_MAGIC, offset)
        if offset < 0:
            return
        decompressor = zlib.decompressobj(16 + zlib.MAX_WBITS)
        chunks = bytearray()
        try:
            for position in range(offset, len(raw), 1 << 16):
                chunks += decompressor.decompress(raw[position : position + (1 << 16)])
                if decompressor.eof:
                    break
        except zlib.error:
            pass  # 半截的段，或這個位置只是剛好長得像段首
        for line in chunks.split(b"\n"):
            if not line.strip():
                continue
            try:
                yield json.loads(line)
            except ValueError:
                pass  # 被中斷時寫到一半的最後一行
        offset += 1


def _moment(milliseconds) -> datetime:
    return datetime.fromtimestamp(milliseconds / 1000, tz=timezone.utc)


def load(directory: pathlib.Path):
    """讀一個爬蟲產出的資料夾，回傳 (地址清單, 特徵矩陣)。

    同一個地址出現多次時以最後一筆為準：續爬可能重抓過它，後面那次比較新。
    """
    manifest = json.loads((directory / "manifest.json").read_text(encoding="utf-8"))
    window_start = _moment(manifest["windowStartMs"])
    window_end = _moment(manifest["windowEndMs"])

    records = {r["address"]: r for r in read_records(directory / "transfers.jsonl.gz")}
    addresses, rows = [], []
    for address, record in records.items():
        transfers = [
            Transfer(t["from"], t["to"], int(t["value"]), _moment(t["ts"]))
            for t in record["transfers"]
        ]
        features = compute(
            transfers,
            address,
            window_start=window_start,
            window_end=window_end,
            truncated=record["truncated"],
        )
        addresses.append(address)
        rows.append(vector(features))
    return addresses, rows


def describe(directory: pathlib.Path) -> dict:
    """訓練 manifest 要記的那幾個設定。特徵只在同一組設定下可比。"""
    manifest = json.loads((directory / "manifest.json").read_text(encoding="utf-8"))
    return {
        "windowStartMs": manifest["windowStartMs"],
        "windowEndMs": manifest["windowEndMs"],
        "windowDays": manifest["windowDays"],
        "transferCap": manifest["transferCap"],
        "featureNames": list(FEATURE_NAMES),
    }
