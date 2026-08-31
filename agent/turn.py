"""一個對話回合的邏輯。

tool loop 由 Go 驅動：我們回 tool_calls，Go 去執行並審核，再帶 tool_results 回來。
這兩次呼叫之間的狀態存在 session 裡。
"""

from llm.base import LLM
from llm.types import Message, Reply
from prompt import build_system_prompt
from schemas import ChatRequest
from session import SessionStore


class SessionExpired(RuntimeError):
    """續跑 tool loop 時找不到 session。不是錯誤，Go 會帶完整 payload 重打。"""


class InvalidTurn(ValueError):
    """第一次呼叫少了 evidence 或 messages。"""


def _resume(store: SessionStore, request: ChatRequest):
    """接回 tool loop 中途的 session，並附上 Go 執行完的工具結果。"""
    session = store.get(request.session_id, request.dataset_id)
    if session is None:
        raise SessionExpired(request.session_id)
    session.pending.extend(result.to_message() for result in request.tool_results or [])
    return session


def _open(store: SessionStore, request: ChatRequest):
    """開一輪新的：證據與對話歷史都由 Go 這次帶來。"""
    if request.evidence is None:
        raise InvalidTurn("第一次呼叫必須帶 evidence")
    return store.start(
        request.session_id,
        request.dataset_id,
        build_system_prompt(request.mode, request.evidence),
        [message.to_message() for message in request.messages or []],
        tuple(spec.to_spec() for spec in request.tools or []),
    )


def run_turn(
    *, store: SessionStore, llm: LLM, request: ChatRequest, max_tokens: int = 4096
) -> Reply:
    session = _resume(store, request) if request.is_continuation else _open(store, request)

    reply = llm.complete(
        system=session.system_prompt,
        messages=session.pending,
        tools=session.tools,
        max_tokens=max_tokens,
    )

    if reply.tool_calls:
        # 記下模型這一手，Go 執行完回填 tool_results 時才接得上。
        # provider_steps 一起帶著：Gemini 要靠裡面的簽章才肯收工具結果。
        session.pending.append(
            Message(
                role="assistant",
                content=reply.text,
                tool_calls=reply.tool_calls,
                provider_steps=reply.provider_steps,
            )
        )
    else:
        # 一輪結束。對話歷史的 source of truth 是 Go 的資料庫，這裡不留。
        session.pending.clear()
    return reply
