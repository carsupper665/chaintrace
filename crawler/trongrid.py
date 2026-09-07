"""TronGrid 讀取用的極薄 client。

只用標準函式庫 —— 這支要能 scp 到任何一台有 Python 的機器就跑起來，不該為了
一個批次腳本去裝 requests 或 httpx。呼叫方式沿用 ChainTrace Go 後端
（analysis/trongrid.go）已經在線上驗證過的那一套。

兩個實測結論（2026-09-01）：
  1. 合約事件端點回傳的地址是 hex（0x1cace…），但抓轉帳的端點只吃 Base58（T…）。
     中間一定要轉，to_base58() 就是為此存在。
"""

import hashlib
import json
import time
import urllib.error
import urllib.parse
import urllib.request

BASE_URL = "https://api.trongrid.io"
USDT_CONTRACT = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
API_KEY_HEADER = "TRON-PRO-API-KEY"

PAGE_LIMIT = 200
# 免費方案有速率限制。慢一點總比被擋在門外重跑一遍好。
REQUEST_INTERVAL_SECONDS = 0.25
MAX_RETRIES = 4

BASE58_ALPHABET = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
TRON_ADDRESS_PREFIX = b"\x41"


class TronGridError(RuntimeError):
    pass


def to_base58(hex_address: str) -> str:
    """把事件回傳的 hex 地址轉成 Base58Check（T…）。

    TRON 地址是 0x41 + 20 bytes，後面接雙 SHA256 的前 4 bytes 當校驗碼。
    """
    payload = TRON_ADDRESS_PREFIX + bytes.fromhex(hex_address.removeprefix("0x"))
    checksum = hashlib.sha256(hashlib.sha256(payload).digest()).digest()[:4]
    number = int.from_bytes(payload + checksum, "big")
    encoded = ""
    while number:
        number, remainder = divmod(number, 58)
        encoded = BASE58_ALPHABET[remainder] + encoded
    return encoded


class TronGrid:
    def __init__(self, api_key: str, *, base_url: str = BASE_URL, interval: float | None = None):
        if not api_key:
            raise TronGridError("缺少 TRONGRID_API_KEY")
        self._api_key = api_key
        self._base_url = base_url.rstrip("/")
        self._interval = REQUEST_INTERVAL_SECONDS if interval is None else interval
        self._next_call_at = 0.0
        self.calls = 0

    def _get(self, path: str, params: dict) -> dict:
        """一次 GET，含速率控制與退避重試。429 與 5xx 值得重試，其他不值得。"""
        url = f"{self._base_url}{path}?{urllib.parse.urlencode(params)}"
        request = urllib.request.Request(
            url, headers={API_KEY_HEADER: self._api_key, "Accept": "application/json"}
        )
        for attempt in range(MAX_RETRIES):
            self._wait_for_slot()
            try:
                with urllib.request.urlopen(request, timeout=30) as response:
                    return json.load(response)
            except urllib.error.HTTPError as error:
                if error.code != 429 and error.code < 500:
                    raise TronGridError(f"TronGrid HTTP {error.code} on {path}") from error
                time.sleep(2**attempt)
            except (urllib.error.URLError, TimeoutError):
                time.sleep(2**attempt)
        raise TronGridError(f"TronGrid 連續 {MAX_RETRIES} 次失敗：{path}")

    def _wait_for_slot(self) -> None:
        delay = self._next_call_at - time.monotonic()
        if delay > 0:
            time.sleep(delay)
        self._next_call_at = time.monotonic() + self._interval
        self.calls += 1

    def sample_addresses(self, count: int, *, before_ms: int, progress=None) -> list[str]:
        """從 before_ms 往回掃 USDT Transfer 事件，收集出現過的地址。

        用時間戳往回走而不是 fingerprint：同一個 before_ms 重跑會抽到同一批地址，
        manifest 才有重現的意義。

        progress 每翻一頁回報一次 —— 抽樣可能要翻十幾頁，沒有回報的話使用者會
        對著一片空白猜它是不是死了。
        """
        seen: dict[str, None] = {}
        cursor = before_ms
        while len(seen) < count and cursor > 0:
            page = self._get(
                f"/v1/contracts/{USDT_CONTRACT}/events",
                {
                    "event_name": "Transfer",
                    "only_confirmed": "true",
                    "limit": PAGE_LIMIT,
                    "order_by": "block_timestamp,desc",
                    "max_block_timestamp": cursor,
                },
            )
            records = page.get("data") or []
            if not records:
                break
            for record in records:
                result = record.get("result") or {}
                for side in ("from", "to"):
                    if result.get(side):
                        seen.setdefault(to_base58(result[side]), None)
            if progress:
                progress(len(seen), self.calls)
            oldest = min(int(r["block_timestamp"]) for r in records)
            if oldest >= cursor:
                break  # 頁面沒有往前推進，再問下去也是同一批
            cursor = oldest - 1
        return list(seen)[:count]

    def transfers(self, address: str, start_ms: int, end_ms: int, cap: int):
        """抓一個地址在窗口內的 USDT 轉帳，最多 cap 筆。

        回傳 (轉帳清單, 是否被 cap 截斷)。截斷與否要往上傳 —— 它是模型的特徵
        之一，不是可以吞掉的細節。
        """
        collected: list[dict] = []
        fingerprint = ""
        while len(collected) < cap:
            params = {
                "only_confirmed": "true",
                "contract_address": USDT_CONTRACT,
                "min_timestamp": start_ms,
                "max_timestamp": end_ms,
                "limit": min(PAGE_LIMIT, cap - len(collected)),
            }
            if fingerprint:
                params["fingerprint"] = fingerprint
            page = self._get(f"/v1/accounts/{address}/transactions/trc20", params)
            records = page.get("data") or []
            collected.extend(_normalize(record) for record in records if _usable(record))
            fingerprint = (page.get("meta") or {}).get("fingerprint", "")
            if not records or not fingerprint:
                return collected, False
        return collected[:cap], True


def _usable(record: dict) -> bool:
    """只收得下的 USDT 轉帳。跟 Go 的收集端同一組條件。"""
    info = record.get("token_info") or {}
    return (
        record.get("type") == "Transfer"
        and info.get("address") == USDT_CONTRACT
        and info.get("decimals") == 6
        and bool(record.get("from"))
        and bool(record.get("to"))
        and str(record.get("value", "")).isdigit()
    )


def _normalize(record: dict) -> dict:
    return {
        "from": record["from"],
        "to": record["to"],
        "value": str(record["value"]),
        "ts": int(record["block_timestamp"]),
    }
