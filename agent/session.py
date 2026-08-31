"""Session 快取。

主要工作是撐住一個 turn 內的 tool loop：Go 呼叫我們拿到 tool_calls、自己去執行、
再呼叫我們一次，這兩次之間的狀態就住在這裡。

這是快取，不是資料庫。重開就沒了是預期行為——對話的 source of truth 在 Go 的
PostgreSQL（見 docs/development-rules.md 第 3 節）。找不到 session 不是錯誤，
回 session_expired 讓 Go 帶完整 payload 重打一次即可。
"""

import time
from collections import OrderedDict
from dataclasses import dataclass, field
from typing import Callable

from llm.types import Message, ToolSpec

DEFAULT_TTL_SECONDS = 1800
DEFAULT_MAX_SESSIONS = 200


@dataclass
class Session:
    investigation_id: str
    """就是 Go 送來的 session_id。"""

    dataset_id: str
    """這個 session 是建立在哪一份 Analysis Dataset 上。換了就得重建。"""

    system_prompt: str
    tools: tuple[ToolSpec, ...] = ()
    """這個 turn 可用的工具。由 Go 提供——它執行工具，所以由它定義。"""

    pending: list[Message] = field(default_factory=list)
    """tool loop 進行中的訊息串。一個 turn 結束就清掉。"""

    last_used: float = 0.0


class SessionStore:
    """TTL + LRU 的記憶體 session 表。

    clock 可注入，測試不用真的 sleep。
    """

    def __init__(
        self,
        *,
        ttl_seconds: int = DEFAULT_TTL_SECONDS,
        max_sessions: int = DEFAULT_MAX_SESSIONS,
        clock: Callable[[], float] = time.monotonic,
    ):
        self._ttl = ttl_seconds
        self._max = max_sessions
        self._clock = clock
        self._sessions: OrderedDict[str, Session] = OrderedDict()

    def __len__(self) -> int:
        return len(self._sessions)

    def get(self, investigation_id: str, dataset_id: str) -> Session | None:
        """取回可用的 session，取不到回 None。

        三種取不到：沒有、過期、或者 dataset 換了（代表跑過新的 Analysis Run，
        證據已經不同，舊的上下文不能再用）。
        """
        session = self._sessions.get(investigation_id)
        if session is None:
            return None
        if self._expired(session):
            del self._sessions[investigation_id]
            return None
        if session.dataset_id != dataset_id:
            del self._sessions[investigation_id]
            return None
        session.last_used = self._clock()
        self._sessions.move_to_end(investigation_id)
        return session

    def start(
        self,
        investigation_id: str,
        dataset_id: str,
        system_prompt: str,
        messages: list[Message] | None = None,
        tools: tuple[ToolSpec, ...] = (),
    ) -> Session:
        """開一個新 session，覆蓋同 id 的舊的。"""
        session = Session(
            investigation_id=investigation_id,
            dataset_id=dataset_id,
            system_prompt=system_prompt,
            tools=tools,
            pending=list(messages or []),
            last_used=self._clock(),
        )
        self._sessions[investigation_id] = session
        self._sessions.move_to_end(investigation_id)
        self._evict()
        return session

    def drop(self, investigation_id: str) -> None:
        self._sessions.pop(investigation_id, None)

    def purge_expired(self) -> int:
        expired = [key for key, s in self._sessions.items() if self._expired(s)]
        for key in expired:
            del self._sessions[key]
        return len(expired)

    def _expired(self, session: Session) -> bool:
        return self._clock() - session.last_used >= self._ttl

    def _evict(self) -> None:
        self.purge_expired()
        while len(self._sessions) > self._max:
            self._sessions.popitem(last=False)
