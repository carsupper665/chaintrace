"""測試用的假 LLM。

Python 這側除了 adapter 翻譯本身以外的測試，全部用這個。
零網路、零金鑰、零成本。
"""

from typing import Sequence

from .types import LLMError, Message, Reply, ToolSpec


class FakeLLM:
    """回傳預先排好的 Reply，並記下每次收到什麼。

    calls 讓測試可以斷言 prompt 組得對不對、工具有沒有正確傳下去。
    """

    def __init__(self, replies: Sequence[Reply] | None = None):
        self._replies = list(replies or [])
        self.calls: list[dict] = []

    def complete(
        self,
        *,
        system: str,
        messages: Sequence[Message],
        tools: Sequence[ToolSpec] = (),
        schema: dict | None = None,
        max_tokens: int = 4096,
    ) -> Reply:
        self.calls.append(
            {
                "system": system,
                "messages": list(messages),
                "tools": list(tools),
                "schema": schema,
                "max_tokens": max_tokens,
            }
        )
        if not self._replies:
            raise LLMError("FakeLLM 沒有更多預設回覆了")
        return self._replies.pop(0)
