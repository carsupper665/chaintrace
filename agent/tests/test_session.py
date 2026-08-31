"""Session 快取測試。時鐘可注入，不用真的等。"""

from llm.types import Message
from session import SessionStore


class Clock:
    def __init__(self):
        self.now = 0.0

    def __call__(self) -> float:
        return self.now

    def advance(self, seconds: float) -> None:
        self.now += seconds


def store(**kwargs) -> SessionStore:
    kwargs.setdefault("clock", Clock())
    return SessionStore(**kwargs)


def test_start_then_get_round_trips():
    clock = Clock()
    s = SessionStore(clock=clock)
    s.start("inv1", "ds1", "SYS", [Message("user", "hi")])
    got = s.get("inv1", "ds1")
    assert got is not None
    assert got.system_prompt == "SYS"
    assert got.pending[0].content == "hi"


def test_missing_session_is_not_an_error():
    assert store().get("nope", "ds1") is None


def test_session_survives_just_under_the_ttl():
    clock = Clock()
    s = SessionStore(ttl_seconds=100, clock=clock)
    s.start("inv1", "ds1", "SYS")
    clock.advance(99)
    assert s.get("inv1", "ds1") is not None


def test_session_expires_after_ttl():
    # 中間不呼叫 get，否則 TTL 會被刷新（那是 test_use_refreshes_the_ttl 在驗的）。
    clock = Clock()
    s = SessionStore(ttl_seconds=100, clock=clock)
    s.start("inv1", "ds1", "SYS")
    clock.advance(100)
    assert s.get("inv1", "ds1") is None
    assert len(s) == 0


def test_use_refreshes_the_ttl():
    clock = Clock()
    s = SessionStore(ttl_seconds=100, clock=clock)
    s.start("inv1", "ds1", "SYS")
    clock.advance(60)
    assert s.get("inv1", "ds1") is not None
    clock.advance(60)
    assert s.get("inv1", "ds1") is not None


def test_new_dataset_drops_the_old_session():
    # 跑過新的 Analysis Run，證據換了，舊上下文不能再用。
    s = store()
    s.start("inv1", "ds1", "SYS")
    assert s.get("inv1", "ds2") is None
    assert len(s) == 0


def test_evicts_oldest_when_over_capacity():
    clock = Clock()
    s = SessionStore(max_sessions=2, clock=clock)
    s.start("a", "ds", "SYS")
    s.start("b", "ds", "SYS")
    s.start("c", "ds", "SYS")
    assert len(s) == 2
    assert s.get("a", "ds") is None
    assert s.get("b", "ds") is not None
    assert s.get("c", "ds") is not None


def test_recently_used_survives_eviction():
    clock = Clock()
    s = SessionStore(max_sessions=2, clock=clock)
    s.start("a", "ds", "SYS")
    s.start("b", "ds", "SYS")
    s.get("a", "ds")          # a 變成最近使用
    s.start("c", "ds", "SYS")  # 該被淘汰的是 b
    assert s.get("a", "ds") is not None
    assert s.get("b", "ds") is None


def test_purge_expired_reports_count():
    clock = Clock()
    s = SessionStore(ttl_seconds=10, clock=clock)
    s.start("a", "ds", "SYS")
    s.start("b", "ds", "SYS")
    clock.advance(11)
    assert s.purge_expired() == 2
    assert len(s) == 0


def test_drop_removes_session():
    s = store()
    s.start("inv1", "ds1", "SYS")
    s.drop("inv1")
    assert s.get("inv1", "ds1") is None
