#!/usr/bin/env python3
"""抓 Tether 在 TRON 上的完整凍結名單（USDT 合約的 AddedBlackList 事件）。

    python freezes.py --out freezes.json

輸出 {地址: 第一次被凍結的毫秒時間戳}，給驗證用：窗口結束之後才被凍結的地址，
應該集中在異常排名前段。時間戳一定要留著 —— 只拿名單不看時間，就分不出「事後才被
凍結」和「早就凍住、窗口內根本不能動」，後者拿來驗證等於在測模型認不認得「沒有活動」。
"""

import argparse
import json
import pathlib

from crawl import api_keys
from trongrid import PAGE_LIMIT, USDT_CONTRACT, TronGrid, TronGridError, to_base58

HERE = pathlib.Path(__file__).resolve().parent


def main() -> None:
    parser = argparse.ArgumentParser(description="抓 USDT 凍結名單")
    parser.add_argument("--out", type=pathlib.Path, default=HERE / "freezes.json")
    arguments = parser.parse_args()

    keys = api_keys()
    if not keys:
        raise TronGridError("缺少 TRONGRID_API_KEY")
    client = TronGrid(keys[0])
    frozen: dict[str, int] = {}
    cursor = None
    while True:
        params = {
            "event_name": "AddedBlackList",
            "only_confirmed": "true",
            "limit": PAGE_LIMIT,
            "order_by": "block_timestamp,desc",
        }
        if cursor:
            params["max_block_timestamp"] = cursor
        records = client._get(f"/v1/contracts/{USDT_CONTRACT}/events", params).get("data") or []
        if not records:
            break
        for record in records:
            address = to_base58(record["result"]["_user"])
            moment = int(record["block_timestamp"])
            frozen[address] = min(moment, frozen.get(address, moment))
        oldest = min(int(r["block_timestamp"]) for r in records)
        if cursor and oldest >= cursor:
            break  # 頁面沒有往前推進
        cursor = oldest - 1
        print(f"  {len(frozen)} 個地址", flush=True)

    arguments.out.write_text(json.dumps(frozen), encoding="utf-8")
    print(f"完成：{len(frozen)} 個凍結地址 → {arguments.out}")


if __name__ == "__main__":
    main()
