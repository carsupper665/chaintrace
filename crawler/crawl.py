#!/usr/bin/env python3
"""爬一批 TRON 地址的 USDT 轉帳，當作異常偵測模型的訓練資料。

    python crawl.py --addresses 1000

抽樣是從最近的 USDT Transfer 流量往回掃，不是從種子地址展開。種子展開會訓練出
「跟我們已經調查過的地址很像的東西」，那正是我們想判斷的對象，不是判斷它的基準。

往回掃抽到的地址，必然在抽樣起點前一刻剛好有交易 —— 實測 100% 在窗口最後一小時
內活躍，而被凍結的地址只有 12.5%。拿它當對照組，模型學到的是「抽樣時剛好活躍」，
不是「可疑」。對照組要用 --spread：在整個窗口內隨機挑時間點抽樣。

TRONGRID_API_KEY 可以給多把，逗號隔開；每把 key 一條執行緒並行爬。

窗口 30 天，轉帳上限 10,000。這兩個數字寫進 manifest，而且**一旦開爬就不再改**：
線上評分必須用同一組設定算特徵，否則模型會在正式環境安靜地瞎猜。

只用標準函式庫，Python 3.10 以上即可。部署方式見 README.md。
"""

import argparse
import gzip
import json
import os
import pathlib
import sys
import threading
import time
import zlib
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timedelta, timezone

from trongrid import USDT_CONTRACT, TronGrid, TronGridError

WINDOW_DAYS = 30
TRANSFER_CAP = 10_000
HERE = pathlib.Path(__file__).resolve().parent


def api_keys() -> list[str]:
    """環境變數優先，其次讀這個資料夾裡的 .env。多把 key 用逗號隔開。"""
    raw = os.getenv("TRONGRID_API_KEY", "").strip()
    local = HERE / ".env"
    if not raw and local.exists():
        for line in local.read_text(encoding="utf-8", errors="ignore").splitlines():
            name, _, value = line.partition("=")
            if name.strip() == "TRONGRID_API_KEY":
                raw = value.strip().strip('"').strip("'")
    return list(dict.fromkeys(k.strip() for k in raw.split(",") if k.strip()))


GZIP_MEMBER_MAGIC = b"\x1f\x8b\x08"


def read_lines(path: pathlib.Path):
    """把資料檔裡讀得出來的每一行吐出來，跳過壞掉的段落。

    不能直接用 gzip.open。append 模式下每執行一次就是一個獨立的 gzip 段，只要
    有一段被中斷（爬蟲被砍、檔案在寫的時候被複製走），gzip 模組讀到它就整個放棄，
    後面完好的段一筆都拿不到。

    實測踩過：一個 24 MB、裡面有 134 MB 完整資料的檔案，被 gzip 模組判成空的。
    這裡改成自己掃段落起點，每段獨立解壓，能讀多少算多少。
    """
    if not path.exists():
        return
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
            pass  # 這一段是半截的，或這個位置只是壓縮資料裡剛好長得像段首
        for line in chunks.split(b"\n"):
            if line.strip():
                yield line
        offset += 1


def finished_addresses(path: pathlib.Path) -> set[str]:
    """從資料檔本身回推已完成的地址，比信任 manifest 可靠。"""
    done: set[str] = set()
    for line in read_lines(path):
        try:
            done.add(json.loads(line)["address"])
        except ValueError:
            pass  # 被砍時寫到一半的最後一行，損失一個地址而不是整批
    return done


def open_manifest(path: pathlib.Path) -> dict:
    """讀既有 manifest，沒有就建一個並**立刻落地**。

    窗口與抽樣起點必須在第一次呼叫之前就寫下來。爬到一半被砍而 manifest 還沒寫，
    續爬就會換一個窗口，兩批資料算出來的特徵不能混用 —— 這是實測踩過的坑。
    """
    if path.exists():
        return json.loads(path.read_text(encoding="utf-8"))
    now = datetime.now(tz=timezone.utc)
    manifest = {
        "crawledAt": now.isoformat(),
        "source": "trongrid contract events, Transfer, walked backwards by timestamp",
        "contract": USDT_CONTRACT,
        "sampleStartMs": int(now.timestamp() * 1000),
        "windowStartMs": int((now - timedelta(days=WINDOW_DAYS)).timestamp() * 1000),
        "windowEndMs": int(now.timestamp() * 1000),
        "windowDays": WINDOW_DAYS,
        "transferCap": TRANSFER_CAP,
        "addressCount": 0,
    }
    save_manifest(path, manifest)
    return manifest


def save_manifest(path: pathlib.Path, manifest: dict) -> None:
    path.write_text(json.dumps(manifest, ensure_ascii=False, indent=1), encoding="utf-8")


def clock(seconds: float) -> str:
    minutes, seconds = divmod(int(seconds), 60)
    hours, minutes = divmod(minutes, 60)
    return f"{hours}:{minutes:02d}:{seconds:02d}" if hours else f"{minutes}:{seconds:02d}"


def crawl(clients: list[TronGrid], addresses: list[str], manifest: dict, out, on_done) -> None:
    """逐地址抓轉帳，一行一個地址寫出去。每把 key 一條執行緒。

    抓取並行，寫檔只在主執行緒：gzip 段不能被兩條執行緒交錯寫。

    每一個地址都回報一次。一個地址平均要十次 API 呼叫，如果幾十個才印一行，
    使用者會盯著一片空白好幾分鐘，分不出是在跑還是掛了。
    """
    start_ms, end_ms = manifest["windowStartMs"], manifest["windowEndMs"]
    cap = manifest["transferCap"]
    total = len(addresses)
    began = time.monotonic()
    idle = list(clients)
    lock = threading.Lock()

    def fetch(address):
        with lock:
            client = idle.pop()
        try:
            return client.transfers(address, start_ms, end_ms, cap)
        finally:
            with lock:
                idle.append(client)

    with ThreadPoolExecutor(max_workers=len(clients)) as pool:
        futures = {pool.submit(fetch, address): address for address in addresses}
        for index, future in enumerate(as_completed(futures), start=1):
            address = futures[future]
            try:
                transfers, truncated = future.result()
            except TronGridError as error:
                print(f"[{index}/{total}] {address} 跳過：{error}", file=sys.stderr, flush=True)
                continue
            out.write(
                json.dumps(
                    {
                        "address": address,
                        "truncated": truncated,
                        "transfers": transfers,
                    },
                    ensure_ascii=False,
                )
                + "\n"
            )
            # 每筆都 flush 並更新 manifest：多花一點 I/O，換到中途被砍也不會掉資料，
            # 以及使用者 ls 得到會長大的檔案。
            out.flush()
            on_done(index)
            elapsed = time.monotonic() - began
            print(
                f"[{index}/{total}] {address}  {len(transfers):>6,} 筆"
                f"{'(截斷)' if truncated else '     '}"
                f"  呼叫 {sum(c.calls for c in clients):>6,}  已跑 {clock(elapsed)}"
                f"  預計還要 {clock(elapsed / index * (total - index))}",
                flush=True,
            )


def sample_spread(clients: list[TronGrid], count: int, manifest: dict, progress) -> list[str]:
    """分散抽樣，所有連線一起抽。結果照 draw 順序合併，跟並行度無關。"""
    pool_size = len(clients)
    idle = list(clients)
    lock = threading.Lock()

    def draw(index):
        with lock:
            client = idle.pop()
        try:
            return client.addresses_at(
                index,
                start_ms=manifest["windowStartMs"],
                end_ms=manifest["windowEndMs"],
                seed=manifest["sampleStartMs"],
            )
        finally:
            with lock:
                idle.append(client)

    seen: dict[str, None] = {}
    next_draw = 0
    with ThreadPoolExecutor(max_workers=pool_size) as pool:
        while len(seen) < count and next_draw < count * 4:
            batch = range(next_draw, next_draw + pool_size * 4)
            next_draw = batch.stop
            for found in pool.map(draw, batch):
                for address in found:
                    seen.setdefault(address, None)
            progress(len(seen), sum(c.calls for c in clients))
    return list(seen)[:count]


def main() -> None:
    parser = argparse.ArgumentParser(description="爬 TRON USDT 地址當訓練資料")
    parser.add_argument("--addresses", type=int, default=1000, help="總目標地址數")
    parser.add_argument("--out", type=pathlib.Path, default=HERE / "data")
    parser.add_argument("--interval", type=float, default=None, help="每次呼叫間隔秒數")
    parser.add_argument(
        "--from-file",
        type=pathlib.Path,
        help="改爬檔案裡列出的地址（一行一個）而不是抽樣。"
        " 標籤集要用這個，並先把訓練資料的 manifest.json 複製到 --out，"
        " 兩邊才會用同一個時間窗口。",
    )
    parser.add_argument(
        "--workers-per-key",
        type=int,
        default=1,
        help="每把 key 同時開幾條連線。瓶頸是網路延遲不是限流時，調大能直接加速。",
    )
    parser.add_argument(
        "--spread",
        action="store_true",
        help="在整個窗口內隨機挑時間點抽樣，而不是從抽樣起點往回掃。對照組要用這個。",
    )
    arguments = parser.parse_args()

    arguments.out.mkdir(parents=True, exist_ok=True)
    manifest_path = arguments.out / "manifest.json"
    transfers_path = arguments.out / "transfers.jsonl.gz"

    print(
        f"啟動：目標 {arguments.addresses} 個地址，窗口 {WINDOW_DAYS} 天，"
        f"每個地址最多 {TRANSFER_CAP:,} 筆轉帳",
        flush=True,
    )
    print(f"輸出：{arguments.out}", flush=True)

    manifest = open_manifest(manifest_path)
    done = finished_addresses(transfers_path)
    if done:
        print(f"沿用既有資料：已完成 {len(done)} 個地址", flush=True)

    keys = api_keys()
    if not keys:
        raise TronGridError("缺少 TRONGRID_API_KEY")
    clients = [
        TronGrid(key, interval=arguments.interval)
        for key in keys
        for _ in range(arguments.workers_per_key)
    ]
    client = clients[0]
    print(f"使用 {len(keys)} 把 API key，共 {len(clients)} 條連線並行", flush=True)

    if arguments.from_file:
        listed = [
            line.strip()
            for line in arguments.from_file.read_text(encoding="utf-8").splitlines()
            if line.strip()
        ]
        pending = [a for a in dict.fromkeys(listed) if a not in done]
        print(f"清單有 {len(listed)} 個地址，其中 {len(pending)} 個要爬\n", flush=True)
    else:
        remaining = max(arguments.addresses - len(done), 0)
        if not remaining:
            print(f"已經有 {len(done)} 個地址，達到目標 {arguments.addresses}。")
            return
        print(f"抽樣中（還缺 {remaining} 個地址，每翻一頁回報一次）…", flush=True)
        progress = lambda found, calls: print(  # noqa: E731
            f"  已抽到 {found} 個地址（{calls} 次呼叫）", flush=True
        )
        if arguments.spread:
            manifest["sampling"] = "spread"
            save_manifest(manifest_path, manifest)
            sampled = sample_spread(clients, arguments.addresses + len(done), manifest, progress)
        else:
            sampled = client.sample_addresses(
                arguments.addresses + len(done),
                before_ms=manifest["sampleStartMs"],
                progress=progress,
            )
        pending = [a for a in sampled if a not in done][:remaining]
        print(f"開始爬 {len(pending)} 個地址\n", flush=True)
    if not pending:
        print("沒有要爬的地址。")
        return

    def record_progress(offset: int) -> None:
        manifest["addressCount"] = len(done) + offset
        save_manifest(manifest_path, manifest)

    with gzip.open(transfers_path, "at", encoding="utf-8") as out:
        crawl(clients, pending, manifest, out, record_progress)

    manifest["addressCount"] = len(finished_addresses(transfers_path))
    save_manifest(manifest_path, manifest)
    print(
        f"完成：{manifest['addressCount']} 個地址，"
        f"共 {sum(c.calls for c in clients)} 次 TronGrid 呼叫"
    )
    print(f"  {transfers_path}\n  {manifest_path}")


if __name__ == "__main__":
    main()
