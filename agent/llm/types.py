"""供應商中性的 LLM 型別。

這個模組不准 import 任何供應商 SDK。adapter 進去出來都是這裡的型別，
上層永遠看不到 google.genai / openai / anthropic 的物件。
"""

from dataclasses import dataclass, field
from typing import Literal

Role = Literal["user", "assistant", "tool"]
StopReason = Literal["stop", "tool_calls", "length", "refusal"]


@dataclass(frozen=True)
class ToolCall:
    """模型要求呼叫一個工具。

    arguments 一律是已解析的 dict。OpenAI 回的是 JSON 字串，
    由該 adapter 負責解析，不外洩給上層。
    """

    id: str
    name: str
    arguments: dict


@dataclass(frozen=True)
class ToolSpec:
    name: str
    description: str
    parameters: dict
    """JSON Schema，描述這個工具吃什麼參數。"""


@dataclass(frozen=True)
class Message:
    """對話中的一則訊息。

    system 不是一種 role，因為三家擺的位置都不同；它是 complete() 的獨立參數。
    """

    role: Role
    content: str = ""
    tool_calls: tuple[ToolCall, ...] = ()
    tool_call_id: str = ""
    """role == "tool" 時，這則是在回覆哪一個 ToolCall。"""

    name: str = ""
    """role == "tool" 時的工具名稱。Gemini 的 function_result 需要它。"""

    is_error: bool = False
    """role == "tool" 時，這次工具執行是否失敗。"""

    provider_steps: tuple = ()
    """供應商自己需要回放的原始步驟，對其他人是不透明的。

    Gemini 3.x 要求把模型那一手的 thought 簽章與 function_call 原樣送回，
    否則工具結果會被拒（400: missing thought_signature）。這裡存的是純
    dict，不是 SDK 物件，所以供應商型別依然沒有外洩；不需要的 adapter
    直接忽略這個欄位。
    """


@dataclass(frozen=True)
class Usage:
    input_tokens: int = 0
    output_tokens: int = 0
    thought_tokens: int = 0
    """模型思考用掉的 token。它會佔用 max_tokens 的額度，所以要看得見。"""


@dataclass(frozen=True)
class Reply:
    text: str = ""
    tool_calls: tuple[ToolCall, ...] = ()
    stop_reason: StopReason = "stop"
    usage: Usage = field(default_factory=Usage)

    provider_steps: tuple = ()
    """模型這一手的原始步驟，續跑時要原樣送回。見 Message.provider_steps。"""


class LLMError(RuntimeError):
    """供應商呼叫失敗。adapter 把各家的例外都收斂成這一種。"""

    def __init__(self, message: str, *, retryable: bool = False):
        super().__init__(message)
        self.retryable = retryable
