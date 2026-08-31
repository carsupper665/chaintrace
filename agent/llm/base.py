"""LLM adapter 介面。

只有一個方法。串流不做——Go 那側要的是一個完整回合，沒有接收端。
tool loop 也不在這裡跑，loop 由 Go 執行（見 docs/development-rules.md 第 6 節）。
"""

from typing import Protocol, Sequence, runtime_checkable

from .types import Message, Reply, ToolSpec


@runtime_checkable
class LLM(Protocol):
    def complete(
        self,
        *,
        system: str,
        messages: Sequence[Message],
        tools: Sequence[ToolSpec] = (),
        schema: dict | None = None,
        max_tokens: int = 4096,
    ) -> Reply:
        """跑一個回合。

        schema 給定時要求結構化輸出，回傳的 Reply.text 是符合該 JSON Schema 的
        JSON 字串。tools 與 schema 不保證能同時使用，呼叫端擇一。
        """
        ...
