"""測試用的假 LLM。

Python 這側除了 adapter 翻譯本身以外的測試，全部用這個。
零網路、零金鑰、零成本。
"""

from typing import Any, Iterable, Sequence

from .base import BaseLLM, Step
from .types import LLMError, Message, Reply, ToolSpec, Usage


class FakeLLM(BaseLLM):
    """回傳預先排好的 Reply，並記下每次收到什麼。

    Message / ToolSpec 本身就是它的 wire format，所以 format hook 全是 identity；
    排好的 Reply 就是它的 payload，reply hook 只是把欄位讀回來讓 base 重組一個
    相等的 Reply。這樣每個用 FakeLLM 的測試都順便走過 base 的骨架。

    calls 讓測試可以斷言 prompt 組得對不對、工具有沒有正確傳下去。它記的是
    build_input 的結果，所以帶 provider_steps 的 assistant 訊息會以回放的步驟
    出現——跟每個 adapter 的規則一樣。
    """

    provider = "Fake"

    def __init__(self, replies: Sequence[Reply] | None = None):
        super().__init__(model="fake")
        self._replies = list(replies or [])
        self.calls: list[dict] = []

    def _make_client(self) -> None:
        return None  # 假的沒有 SDK，也永遠不會被叫到

    # ---- request ----

    def format_user(self, message: Message) -> Step:
        return message

    def format_assistant(self, message: Message) -> list[Step]:
        return [message]

    def format_tool_result(self, message: Message) -> Step:
        return message

    def format_tool(self, tool: ToolSpec) -> Step:
        return tool

    def build_request(
        self,
        *,
        system: str,
        messages: Sequence[Message],
        tools: Sequence[ToolSpec] = (),
        schema: dict | None = None,
        max_tokens: int = 4096,
    ) -> dict:
        request = {
            "system": system,
            "messages": self.build_input(messages),
            "tools": self.build_tools(tools),
            "schema": schema,
            "max_tokens": max_tokens,
        }
        self.calls.append(request)
        return request

    def _send(self, request: dict) -> Reply:
        if not self._replies:
            raise LLMError("FakeLLM 沒有更多預設回覆了")
        return self._replies.pop(0)

    # ---- reply ----

    def text_of(self, reply: Reply) -> str:
        return reply.text

    def raw_tool_calls(self, reply: Reply) -> Iterable[tuple[str, str, Any]]:
        return [(call.id, call.name, call.arguments) for call in reply.tool_calls]

    def usage_of(self, reply: Reply) -> Usage:
        return reply.usage

    def is_truncated(self, reply: Reply) -> bool:
        return reply.stop_reason == "length"

    def is_refused(self, reply: Reply) -> bool:
        return reply.stop_reason == "refusal"

    def replayable_steps(self, reply: Reply) -> tuple:
        return reply.provider_steps
