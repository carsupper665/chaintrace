"""Address Feature Vector —— 一個地址的 22 項行為特徵。

這個模組是整個學習式評分的地基（見 docs/adr/0015）。訓練和線上打分**共用這一份
程式碼**：兩邊如果各算各的，模型在線上會照樣吐出一個看起來合理的分數，而你永遠
不會發現它其實在瞎猜。

三條規則決定這個模型會不會成功：

  1. 金額全程用 int（最小單位），只在最後一步套 log1p。中途轉 float 會在
     2^53 以上悄悄失真，而 USDT 的最小單位很容易超過。
  2. 盡量用比例而不是絕對值。絕對值會隨收集上限浮動，比例不會。
  3. 「有沒有被截斷」是一個特徵，不是一個要藏起來的祕密。

這裡不連網、不碰資料庫、不載入模型。first_seen 是**參數不是查詢** —— 它要多打
一次 API，但那是呼叫端的責任。
"""

import math
import statistics
from collections import Counter, deque
from dataclasses import dataclass
from datetime import datetime

# TRC20 USDT 固定 6 位小數；收集端也是照這個過濾的（analysis/collector.go）。
USDT_DECIMALS = 6

# 「快速過水」的門檻：配對到的資金在多少秒內就被轉出。
PASS_THROUGH_SECONDS = 3600

# 「進出金額配對」的搜尋窗與容差：一筆流入之後多久內、金額差多少以內，
# 算是同一筆錢被轉手出去。
MATCH_WINDOW_SECONDS = 24 * 3600
MATCH_TOLERANCE = 0.02

# 「整數金額」的粒度：多少個主單位的整數倍才算整數金額。
ROUND_UNIT = 100

HOUR_BUCKETS = 24

FEATURE_NAMES: tuple[str, ...] = (
    # 活躍、金額與資金流
    "in_count_log",
    "out_count_log",
    "in_count_share",
    "net_flow_ratio",
    "median_amount_log",
    "amount_cv",
    "max_inflow_share",
    "pass_through_share",
    "median_holding_seconds_log",
    # 對手方分散／集中
    "in_counterparties_log",
    "out_counterparties_log",
    "in_hhi",
    "out_hhi",
    "counterparty_reuse_rate",
    # 時間節奏與金額規律
    "active_day_ratio",
    "gap_cv",
    "hour_entropy",
    "round_amount_share",
    "repeated_amount_share",
    # 這三項是原始清單沒有、但值得加的
    "address_age_log",
    "flow_match_share",
    "structuring_share",
    # 覆蓋率本身
    "truncated",
)


@dataclass(frozen=True)
class Transfer:
    """一筆 TRC20 轉帳。amount 是最小單位的整數，不是 float。"""

    from_address: str
    to_address: str
    amount: int
    timestamp: datetime


def compute(
    transfers,
    target: str,
    *,
    window_start: datetime,
    window_end: datetime,
    truncated: bool = False,
    first_seen: datetime | None = None,
    decimals: int = USDT_DECIMALS,
) -> dict[str, float]:
    """算出 target 的特徵。回傳的鍵剛好是 FEATURE_NAMES。"""
    inflow, outflow = _split(transfers, target)
    own = inflow + outflow
    return {
        **_flow(inflow, outflow),
        **_counterparties(inflow, outflow),
        **_rhythm(own, window_start, window_end, decimals, truncated),
        **_extras(inflow, outflow, first_seen, window_end),
        "truncated": 1.0 if truncated else 0.0,
    }


def vector(features: dict[str, float]) -> tuple[float, ...]:
    """把特徵字典攤平成固定順序的向量。FEATURE_NAMES 是唯一的順序來源。"""
    return tuple(float(features[name]) for name in FEATURE_NAMES)


def _split(transfers, target: str):
    """拆成流入與流出。自轉（from == to == target）兩邊都不算，它不是資金流動。"""
    inflow = [t for t in transfers if t.to_address == target and t.from_address != target]
    outflow = [t for t in transfers if t.from_address == target and t.to_address != target]
    return inflow, outflow


def _when(transfer: Transfer) -> datetime:
    return transfer.timestamp


def _flow(inflow: list, outflow: list) -> dict[str, float]:
    total_in = sum(t.amount for t in inflow)
    total_out = sum(t.amount for t in outflow)
    amounts = [t.amount for t in inflow + outflow]
    pairs = _pair_flows(inflow, outflow)
    forwarded = sum(amount for amount, gap in pairs if gap <= PASS_THROUGH_SECONDS)
    gaps = [gap for _, gap in pairs]
    return {
        "in_count_log": _log1p(len(inflow)),
        "out_count_log": _log1p(len(outflow)),
        "in_count_share": _safe_ratio(len(inflow), len(inflow) + len(outflow)),
        "net_flow_ratio": _safe_ratio(total_in - total_out, total_in),
        "median_amount_log": _log1p(_median(amounts)),
        "amount_cv": _cv(amounts),
        "max_inflow_share": _safe_ratio(max((t.amount for t in inflow), default=0), total_in),
        "pass_through_share": _safe_ratio(forwarded, total_in),
        "median_holding_seconds_log": _log1p(_median(gaps)),
    }


def _pair_flows(inflow: list, outflow: list) -> list[tuple[int, float]]:
    """FIFO 配對：每筆流出從最早、且發生在它之前的流入扣抵。

    回傳 (配對金額, 間隔秒數)。快速過水與持有時間都由這一份結果算出來 ——
    配對只做一次，兩個特徵共用。
    """
    queue = deque((t.timestamp, t.amount) for t in sorted(inflow, key=_when))
    pairs: list[tuple[int, float]] = []
    for out in sorted(outflow, key=_when):
        remaining = out.amount
        while remaining > 0 and queue and queue[0][0] <= out.timestamp:
            when, available = queue[0]
            used = min(remaining, available)
            pairs.append((used, (out.timestamp - when).total_seconds()))
            remaining -= used
            if used == available:
                queue.popleft()
            else:
                queue[0] = (when, available - used)
    return pairs


def _counterparties(inflow: list, outflow: list) -> dict[str, float]:
    senders = [t.from_address for t in inflow]
    receivers = [t.to_address for t in outflow]
    appearances = Counter(senders + receivers)
    reused = sum(1 for count in appearances.values() if count >= 2)
    return {
        "in_counterparties_log": _log1p(len(set(senders))),
        "out_counterparties_log": _log1p(len(set(receivers))),
        "in_hhi": _hhi(_amounts_by(inflow, "from_address")),
        "out_hhi": _hhi(_amounts_by(outflow, "to_address")),
        "counterparty_reuse_rate": _safe_ratio(reused, len(appearances)),
    }


def _amounts_by(transfers: list, field: str) -> list[int]:
    totals: Counter = Counter()
    for transfer in transfers:
        totals[getattr(transfer, field)] += transfer.amount
    return list(totals.values())


def _rhythm(
    own: list, window_start: datetime, window_end: datetime, decimals: int, truncated: bool
) -> dict[str, float]:
    times = sorted(t.timestamp for t in own)
    gaps = [(b - a).total_seconds() for a, b in zip(times, times[1:])]
    hours = Counter(t.hour for t in times)
    amounts = [t.amount for t in own]
    repeats = Counter(amounts)
    round_unit = ROUND_UNIT * 10**decimals
    return {
        "active_day_ratio": _safe_ratio(
            len({t.date() for t in times}), _observed_days(times, window_start, window_end, truncated)
        ),
        "gap_cv": _cv(gaps),
        "hour_entropy": _entropy(list(hours.values()), HOUR_BUCKETS),
        "round_amount_share": _safe_ratio(
            sum(1 for a in amounts if a % round_unit == 0), len(amounts)
        ),
        "repeated_amount_share": _safe_ratio(
            sum(count for count in repeats.values() if count >= 2), len(amounts)
        ),
    }


def _observed_days(
    times: list, window_start: datetime, window_end: datetime, truncated: bool
) -> int:
    """活躍天數要除以什麼。

    沒被截斷時就是請求的窗口。被截斷時只能除以實際看到的區間 —— 一個 30 天內轉
    了五萬筆的地址，我們只拿到最近幾天，用 30 天當分母會算出「很少活動」，剛好
    把最該注意的地址講反。至少算 1 天，否則全部擠在同一天的地址會除以零。
    """
    if not truncated or not times:
        return (window_end - window_start).days
    return max((times[-1] - times[0]).days, 1)


def _extras(
    inflow: list, outflow: list, first_seen: datetime | None, window_end: datetime
) -> dict[str, float]:
    age_days = (window_end - first_seen).days if first_seen else 0
    matched = sum(1 for t in inflow if _has_echo(t, outflow))
    structured = sum(1 for t in inflow + outflow if _is_structured(t.amount))
    return {
        "address_age_log": _log1p(max(age_days, 0)),
        "flow_match_share": _safe_ratio(matched, len(inflow)),
        "structuring_share": _safe_ratio(structured, len(inflow) + len(outflow)),
    }


def _has_echo(received: Transfer, outflow: list) -> bool:
    """這筆流入之後，短時間內有沒有一筆金額幾乎相同的流出。

    淨流只比總和：「收 100 轉 100」和「收 1000 轉 900」淨流一樣，行為完全不同。
    逐筆對得起來才是中繼轉手的樣子。
    """
    tolerance = received.amount * MATCH_TOLERANCE
    for sent in outflow:
        elapsed = (sent.timestamp - received.timestamp).total_seconds()
        if 0 <= elapsed <= MATCH_WINDOW_SECONDS and abs(sent.amount - received.amount) <= tolerance:
            return True
    return False


def _is_structured(amount: int) -> bool:
    """金額是不是剛好卡在某個 10 次方的下緣（9,900 這種）。

    用位數而不是對數，全程整數，任何大小都精確。
    """
    if amount <= 0:
        return False
    return amount >= 9 * 10 ** (len(str(amount)) - 1)


def _safe_ratio(numerator, denominator) -> float:
    """所有比例都走這裡。分母為 0 回 0.0，除零保護只寫一次。"""
    return float(numerator) / float(denominator) if denominator else 0.0


def _median(values: list) -> float:
    return statistics.median(values) if values else 0.0


def _cv(values: list) -> float:
    """變異係數。少於兩個值或平均為 0 時沒有離散度可言，回 0.0。"""
    if len(values) < 2:
        return 0.0
    mean = statistics.fmean(values)
    return statistics.pstdev(values) / mean if mean else 0.0


def _hhi(weights: list[int]) -> float:
    """Herfindahl 集中度：份額平方和。1.0 表示全部集中在一個對手方。"""
    total = sum(weights)
    if not total:
        return 0.0
    return sum((weight / total) ** 2 for weight in weights)


def _entropy(counts: list[int], buckets: int) -> float:
    """歸一化 Shannon entropy，落在 0–1。

    用熵而不是「深夜比例」：鏈上時間是 UTC，對方的時區未知，設一個深夜門檻
    等於把猜測寫進特徵裡。熵只問活動集不集中，不需要那個假設。
    """
    total = sum(counts)
    if total <= 0:
        return 0.0
    entropy = -sum((c / total) * math.log(c / total) for c in counts if c)
    return entropy / math.log(buckets)


def _log1p(value) -> float:
    return math.log1p(float(value))
