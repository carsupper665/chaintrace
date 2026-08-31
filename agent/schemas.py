"""Go ↔ Python 的線上格式。

這是 /v1/agent/chat 的合約。新增欄位一律可選，舊版收到不認得的欄位要能忽略
（見 docs/development-rules.md 第 7 節）。
"""

from typing import Literal

from pydantic import BaseModel, Field

from llm.types import Message, Reply, ToolCall, ToolSpec


class ToolCallOut(BaseModel):
    id: str
    name: str
    arguments: dict


class ToolSpecIn(BaseModel):
    name: str
    description: str = ""
    parameters: dict = Field(default_factory=dict)

    def to_spec(self) -> ToolSpec:
        return ToolSpec(name=self.name, description=self.description, parameters=self.parameters)


class MessageIn(BaseModel):
    role: Literal["user", "assistant", "tool"]
    content: str = ""
    tool_calls: list[ToolCallOut] = Field(default_factory=list)
    tool_call_id: str = ""
    name: str = ""
    is_error: bool = False

    def to_message(self) -> Message:
        return Message(
            role=self.role,
            content=self.content,
            tool_calls=tuple(
                ToolCall(id=c.id, name=c.name, arguments=c.arguments) for c in self.tool_calls
            ),
            tool_call_id=self.tool_call_id,
            name=self.name,
            is_error=self.is_error,
        )


class ToolResultIn(BaseModel):
    """Go 執行完一個工具後回填的結果。"""

    call_id: str
    name: str = ""
    content: str = ""
    is_error: bool = False

    def to_message(self) -> Message:
        return Message(
            role="tool",
            content=self.content,
            tool_call_id=self.call_id,
            name=self.name,
            is_error=self.is_error,
        )


class ChatRequest(BaseModel):
    session_id: str
    """就是 Investigation ID。Python 只拿它當 key，不做任何授權判斷。"""

    dataset_id: str
    """證據是建立在哪份 Analysis Dataset 上。換了就重建 session。"""

    mode: Literal["summary", "chat"] = "chat"

    evidence: dict | None = None
    """一輪的第一次呼叫才帶。續跑 tool loop 時省略。"""

    messages: list[MessageIn] | None = None
    """一輪的第一次呼叫才帶，對話歷史由 Go 的資料庫提供。"""

    tools: list[ToolSpecIn] | None = None
    """可用工具由 Go 定義——它執行工具，所以合約歸它。"""

    tool_results: list[ToolResultIn] | None = None
    """帶了這個就是續跑 tool loop。"""

    @property
    def is_continuation(self) -> bool:
        return bool(self.tool_results)


class UsageOut(BaseModel):
    input_tokens: int = 0
    output_tokens: int = 0


class ChatResponse(BaseModel):
    text: str = ""
    tool_calls: list[ToolCallOut] = Field(default_factory=list)
    stop_reason: str = "stop"
    usage: UsageOut = Field(default_factory=UsageOut)

    @classmethod
    def from_reply(cls, reply: Reply) -> "ChatResponse":
        return cls(
            text=reply.text,
            tool_calls=[
                ToolCallOut(id=c.id, name=c.name, arguments=c.arguments) for c in reply.tool_calls
            ],
            stop_reason=reply.stop_reason,
            usage=UsageOut(
                input_tokens=reply.usage.input_tokens, output_tokens=reply.usage.output_tokens
            ),
        )


class ErrorResponse(BaseModel):
    code: str
    message: str
