"""Address Feature Vector 的測試。

每個期望值都是手算的，不是把實作的輸出貼回來 —— 貼回來的測試只會證明程式碼
沒改變，不會證明它算對。
"""

import math
from datetime import datetime, timedelta, timezone

import pytest

from scoring.features import FEATURE_NAMES, Transfer, compute, vector

TARGET = "TARGET"
WINDOW_START = datetime(2026, 8, 1, tzinfo=timezone.utc)
WINDOW_END = datetime(2026, 8, 31, tzinfo=timezone.utc)  # 30 天


def usdt(amount: float) -> int:
    """主單位換算成最小單位（6 位小數）。"""
    return int(amount * 1_000_000)


def at(day: int, hour: int, minute: int = 0) -> datetime:
    return datetime(2026, 8, day, hour, minute, tzinfo=timezone.utc)


# 共用情境。四筆轉帳，數字挑得剛好可以心算：
#   A →TARGET  100 USDT  8/1 10:00
#   B →TARGET  300 USDT  8/2 10:00
#   TARGET→ C  100 USDT  8/1 10:30   （收到後 30 分鐘就轉走）
#   TARGET→ A  200 USDT  8/3 22:00
SCENARIO = [
    Transfer("A", TARGET, usdt(100), at(1, 10)),
    Transfer("B", TARGET, usdt(300), at(2, 10)),
    Transfer(TARGET, "C", usdt(100), at(1, 10, 30)),
    Transfer(TARGET, "A", usdt(200), at(3, 22)),
]


def features(transfers=SCENARIO, **overrides) -> dict[str, float]:
    options = {
        "window_start": WINDOW_START,
        "window_end": WINDOW_END,
        "truncated": False,
        **overrides,
    }
    return compute(transfers, TARGET, **options)


def test_feature_names_are_unique_and_the_vector_follows_them():
    assert len(FEATURE_NAMES) == len(set(FEATURE_NAMES)) == 23

    computed = features()
    assert set(computed) == set(FEATURE_NAMES)

    flattened = vector(computed)
    assert len(flattened) == 23
    assert all(flattened[i] == computed[name] for i, name in enumerate(FEATURE_NAMES))


def test_flow_features():
    computed = features()

    # 2 進 2 出，總流入 400、總流出 300。
    assert computed["in_count_log"] == pytest.approx(math.log1p(2))
    assert computed["out_count_log"] == pytest.approx(math.log1p(2))
    assert computed["in_count_share"] == pytest.approx(2 / 4)
    assert computed["net_flow_ratio"] == pytest.approx((400 - 300) / 400)

    # 金額排序後是 100/100/200/300，中位數 150。
    assert computed["median_amount_log"] == pytest.approx(math.log1p(usdt(150)))

    # 平均 175，母體標準差 sqrt(27500/4) = 82.9156…
    assert computed["amount_cv"] == pytest.approx(math.sqrt(6875) / 175)

    # 最大一筆流入 300 佔總流入 400。
    assert computed["max_inflow_share"] == pytest.approx(300 / 400)


def test_pass_through_and_holding_time_come_from_one_fifo_pairing():
    computed = features()

    # FIFO：100 在 30 分鐘（1800 秒）後被轉走；剩下的 200 從 8/2 10:00 那筆
    # 扣抵，隔了 36 小時（129600 秒）。只有第一筆算「快速過水」。
    assert computed["pass_through_share"] == pytest.approx(100 / 400)
    assert computed["median_holding_seconds_log"] == pytest.approx(
        math.log1p((1800 + 129600) / 2)
    )


def test_counterparty_features():
    computed = features()

    assert computed["in_counterparties_log"] == pytest.approx(math.log1p(2))  # A、B
    assert computed["out_counterparties_log"] == pytest.approx(math.log1p(2))  # C、A

    # 流入集中度：A 佔 1/4、B 佔 3/4 → 0.0625 + 0.5625
    assert computed["in_hhi"] == pytest.approx(0.25**2 + 0.75**2)
    # 流出集中度：C 佔 1/3、A 佔 2/3 → 5/9
    assert computed["out_hhi"] == pytest.approx(5 / 9)

    # 三個對手方裡只有 A 出現兩次。
    assert computed["counterparty_reuse_rate"] == pytest.approx(1 / 3)


def test_rhythm_features():
    computed = features()

    # 8/1、8/2、8/3 三天有活動，窗口 30 天。
    assert computed["active_day_ratio"] == pytest.approx(3 / 30)

    # 間隔依序是 1800、84600、129600 秒；平均 72000。
    gaps = [1800, 84600, 129600]
    mean = sum(gaps) / 3
    deviation = math.sqrt(sum((g - mean) ** 2 for g in gaps) / 3)
    assert computed["gap_cv"] == pytest.approx(deviation / mean)

    # 小時分佈是 10 點三次、22 點一次。
    expected = -(0.75 * math.log(0.75) + 0.25 * math.log(0.25)) / math.log(24)
    assert computed["hour_entropy"] == pytest.approx(expected)

    # 四筆都是 100 USDT 的整數倍；100 出現兩次。
    assert computed["round_amount_share"] == pytest.approx(1.0)
    assert computed["repeated_amount_share"] == pytest.approx(2 / 4)


def test_round_amount_share_rejects_untidy_amounts():
    transfers = [
        Transfer("A", TARGET, usdt(200), at(1, 10)),  # 100 的整數倍
        Transfer("A", TARGET, usdt(137.5), at(2, 10)),  # 不是
    ]
    assert features(transfers)["round_amount_share"] == pytest.approx(0.5)


def test_structuring_share_catches_amounts_parked_under_a_round_threshold():
    transfers = [
        Transfer("A", TARGET, usdt(9900), at(1, 10)),  # 卡在 10,000 下緣
        Transfer("A", TARGET, usdt(5000), at(2, 10)),  # 沒有
    ]
    assert features(transfers)["structuring_share"] == pytest.approx(0.5)
    # 共用情境的 100/200/300 都離下緣很遠。
    assert features()["structuring_share"] == pytest.approx(0.0)


def test_flow_match_share_needs_a_near_equal_outflow_soon_after():
    computed = features()
    # 100 進來 30 分鐘後有一筆等額轉出；300 那筆之後 36 小時才有轉出，超過 24
    # 小時的搜尋窗，而且金額也對不上。
    assert computed["flow_match_share"] == pytest.approx(1 / 2)


def test_address_age_uses_the_supplied_first_seen():
    # first_seen 是參數不是查詢：帳戶年齡要多打一次 API，但那是呼叫端的事。
    assert features()["address_age_log"] == pytest.approx(0.0)

    aged = features(first_seen=WINDOW_END - timedelta(days=400))
    assert aged["address_age_log"] == pytest.approx(math.log1p(400))


def test_truncation_is_reported_not_hidden():
    assert features()["truncated"] == 0.0
    assert features(truncated=True)["truncated"] == 1.0


def test_truncated_rows_measure_activity_against_what_was_actually_seen():
    # 實測發現 38% 的抽樣地址轉帳數超過一頁。這些地址被 cap 砍掉之後，若仍拿
    # 30 天當分母，「天天在動」會被算成「幾乎不動」—— 剛好把最該注意的講反。
    busy = [
        Transfer("A", TARGET, usdt(10), at(1, 9)),
        Transfer("B", TARGET, usdt(10), at(2, 9)),
        Transfer(TARGET, "C", usdt(10), at(3, 9)),
    ]
    # 完整資料：三天活動 ÷ 三十天窗口。
    assert features(busy)["active_day_ratio"] == pytest.approx(3 / 30)
    # 截斷資料：只看得到兩天的區間，而這兩天裡三天都有活動 → 分母是觀測區間。
    assert features(busy, truncated=True)["active_day_ratio"] == pytest.approx(3 / 2)


def test_truncated_rows_crammed_into_one_day_do_not_divide_by_zero():
    same_day = [
        Transfer("A", TARGET, usdt(10), at(1, 9)),
        Transfer("B", TARGET, usdt(10), at(1, 21)),
    ]
    assert features(same_day, truncated=True)["active_day_ratio"] == pytest.approx(1.0)


def test_amounts_above_two_to_the_53_stay_distinct():
    # float 會把 2^53 和 2^53+1 視為同一個數。如果金額中途轉成 float，這兩筆
    # 就會被算成「重複金額」。
    transfers = [
        Transfer("A", TARGET, 2**53, at(1, 10)),
        Transfer("B", TARGET, 2**53 + 1, at(2, 10)),
    ]
    assert features(transfers)["repeated_amount_share"] == pytest.approx(0.0)


@pytest.mark.parametrize(
    "name, transfers",
    [
        ("沒有轉帳", []),
        ("只有流入", [Transfer("A", TARGET, usdt(10), at(1, 10))]),
        ("只有流出", [Transfer(TARGET, "A", usdt(10), at(1, 10))]),
        (
            "金額全部相同",
            [
                Transfer("A", TARGET, usdt(10), at(1, 10)),
                Transfer("A", TARGET, usdt(10), at(2, 10)),
            ],
        ),
        (
            "同一個時間戳",
            [
                Transfer("A", TARGET, usdt(10), at(1, 10)),
                Transfer("B", TARGET, usdt(20), at(1, 10)),
            ],
        ),
        ("與目標無關", [Transfer("A", "B", usdt(10), at(1, 10))]),
        ("自己轉給自己", [Transfer(TARGET, TARGET, usdt(10), at(1, 10))]),
    ],
)
def test_degenerate_inputs_return_defined_values(name, transfers):
    computed = features(transfers)
    assert set(computed) == set(FEATURE_NAMES), name
    for feature, value in computed.items():
        assert isinstance(value, float), f"{name}: {feature}"
        assert math.isfinite(value), f"{name}: {feature} = {value}"


def test_zero_length_window_does_not_divide_by_zero():
    computed = features(window_start=WINDOW_END, window_end=WINDOW_END)
    assert computed["active_day_ratio"] == 0.0


def test_the_same_input_produces_the_same_vector():
    assert vector(features()) == vector(features())
