"""LLM adapter 介面與共用骨架。

兩樣東西：

- `LLM`：呼叫端（turn.py）依賴的介面。`complete()` 跑一個完整回合；`stream()`
  跑同一個回合但邊收邊吐 `StreamChunk`，最後一定以 `StreamDone` 收尾，裡面的
  `reply` 跟 `complete()` 會回的完全一樣，續跑 tool loop 的簿記只看這個。沒有
  真的串流能力的 adapter 用 `complete_as_stream` 墊一個「一次性 chunk」的版本，
  呼叫端不用分辨誰真的在串誰沒有。tool loop 也不在這裡跑，loop 由 Go 執行
  （見 docs/development-rules.md 第 6 節）。
- `BaseLLM`：request/response 型 adapter 的共用實作。共用的是「邏輯」——訊息
  怎麼走、模型那一手怎麼回放、工具怎麼宣告、回覆怎麼組、錯誤怎麼分類；adapter
  只提供各家的 wire format 字面值。Codex 是有狀態的 stdio 協定，不走這個骨架，
  它直接滿足 `LLM` 就好。

這個模組不准 import 任何供應商 SDK。
"""

import json
import os
from abc import ABC, abstractmethod
from typing import Any, Callable, ClassVar, Iterable, Iterator, Protocol, Sequence, runtime_checkable

from .types import (
    LLMError,
    Message,
    Reply,
    StopReason,
    StreamChunk,
    StreamDone,
    TextDelta,
    ToolCall,
    ToolSpec,
    Usage,
)

Step = Any
"""wire format 裡的一則訊息／一個工具宣告，形狀由 adapter 決定。

Gemini 是 Interactions API 的 step dict，FakeLLM 直接用 Message / ToolSpec 本身。
"""


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

    def stream(
        self,
        *,
        system: str,
        messages: Sequence[Message],
        tools: Sequence[ToolSpec] = (),
        schema: dict | None = None,
        max_tokens: int = 4096,
    ) -> Iterator[StreamChunk]:
        """跑同一個回合，邊收邊吐。最後一個 chunk 一定是 StreamDone。

        沒有真的串流能力的 adapter 用 complete_as_stream 墊一個只有一個 chunk
        的版本；呼叫端一律當作真的在串就好。
        """
        ...


# ---- 中性的小工具：不碰 SDK，每家 adapter 都用得上 ----


def complete_as_stream(
    complete: Callable[..., Reply],
    *,
    system: str,
    messages: Sequence[Message],
    tools: Sequence[ToolSpec] = (),
    schema: dict | None = None,
    max_tokens: int = 4096,
) -> Iterator[StreamChunk]:
    """給沒有真的串流能力的 adapter 墊一個 stream()：整段答案一次回來，
    包成一個 TextDelta 再一個 StreamDone，呼叫端不用分辨誰真的在串。

    沒有思考文字可吐——那些 adapter 的 complete() 本來就沒有把思考文字
    從 payload 裡撈出來，這裡也生不出來。
    """
    reply = complete(
        system=system, messages=messages, tools=tools, schema=schema, max_tokens=max_tokens
    )
    if reply.text:
        yield TextDelta(reply.text)
    yield StreamDone(reply)


def function_declaration(tool: ToolSpec) -> dict:
    """工具宣告的三個共通欄位。各家再決定要不要包一層。"""
    return {
        "name": tool.name,
        "description": tool.description,
        "parameters": tool.parameters,
    }


def parse_arguments(raw) -> dict:
    """tool call 的參數統一成 dict。

    Gemini 文件與型別都說是 dict，但收到字串時寧可解析也不要整輪炸掉；
    OpenAI 則一律回 JSON 字串（見 types.ToolCall）。
    """
    if not isinstance(raw, str):
        return raw or {}
    try:
        return json.loads(raw)
    except ValueError as error:
        raise LLMError(f"tool call 參數不是合法 JSON: {error}") from error


def resolve_stop_reason(
    *, tool_calls: tuple[ToolCall, ...], truncated: bool = False, refused: bool = False
) -> StopReason:
    """有 tool call 就是 tool_calls；被切斷絕不能當成正常結束回報——
    上層會把半截的摘要當完整的存進資料庫。"""
    if tool_calls:
        return "tool_calls"
    if truncated:
        return "length"
    if refused:
        return "refusal"
    return "stop"


def dump_failed_request(request: dict) -> None:
    """把被拒絕的請求寫到 AGENT_DEBUG_DUMP 指定的檔案。

    供應商對格式問題常常只回一句「invalid argument」，不指出哪裡錯，所以沒有
    原始 payload 就只能一路猜。預設關閉：payload 含證據內容。
    """
    path = os.getenv("AGENT_DEBUG_DUMP", "").strip()
    if not path:
        return
    try:
        with open(path, "w", encoding="utf-8") as handle:
            json.dump(request, handle, ensure_ascii=False, indent=1, default=str)
    except OSError:
        pass  # 診斷用的旁支，絕不能因此讓真正的錯誤消失


# 錯誤分類表：(名稱, HTTP 狀態碼, 訊息特徵, 是否值得重試, 訊息樣板)。
# 先比狀態碼（openai SDK 放在 status_code、google-genai 放在 code），再退回比字串。
# retryable 決定上層要不要重試，所以 400 這種請求本身有問題的絕不能標成可重試——
# 重試一百次還是同樣的 400。
# 樣板可用 {provider} {hint} {text} {timeout}；hint 由各 adapter 的 error_hints 依名稱補上。
ERROR_RULES: list[tuple[str, tuple[int, ...], tuple[str, ...], bool, str]] = [
    (
        "timeout",
        (),
        ("timed out", "timeout"),
        True,
        "{provider} 在 {timeout:.0f} 秒內沒有回應。可調高 LLM_TIMEOUT_SECONDS{hint}。",
    ),
    (
        "bad_request",
        (400,),
        ("error code: 400", "invalid_request"),
        False,
        "{provider} 拒絕這個請求（設定或參數有誤）{hint}：{text}",
    ),
    (
        "unauthorized",
        (401, 403),
        ("error code: 401", "error code: 403"),
        False,
        "{provider} 拒絕存取，請檢查 LLM_API_KEY{hint}：{text}",
    ),
    (
        "rate_limited",
        (429,),
        ("error code: 429",),
        True,
        "{provider} 額度或速率上限{hint}：{text}",
    ),
    (
        "overloaded",
        (503,),
        ("high demand", "unavailable", "error code: 503"),
        True,
        # 模型過載。重試通常沒用，換一個模型比較快。
        "{provider} 這個模型目前過載{hint}：{text}",
    ),
]


def _status_code(error: Exception) -> int | None:
    for attribute in ("status_code", "code"):
        value = getattr(error, attribute, None)
        if isinstance(value, int) and not isinstance(value, bool):
            return value
    return None


def classify_error(
    error: Exception,
    *,
    provider: str,
    timeout: float,
    hints: dict[str, str] | None = None,
) -> LLMError:
    """把 SDK 例外分成「我們寫錯」和「等一下再試」。"""
    text = str(error)
    lowered = text.lower()
    status = _status_code(error)
    hints = hints or {}
    for name, codes, markers, retryable, template in ERROR_RULES:
        if status in codes or any(marker in lowered for marker in markers):
            message = template.format(
                provider=provider, hint=hints.get(name, ""), text=text, timeout=timeout
            )
            return LLMError(message, retryable=retryable)
    return LLMError(f"{provider} 呼叫失敗: {text}", retryable=True)


# ---- 共用骨架 ----


class BaseLLM(ABC):
    """Request/response 型 adapter 的共用骨架。

    子類別只填字面值：每個 role 在 wire format 裡長什麼樣、request 的鍵叫什麼、
    回覆的欄位在哪。走訪、回放、組 Reply、分類錯誤都在這裡，各家一致。
    """

    provider: ClassVar[str]
    """錯誤訊息裡用的名字，例如 "Gemini"。"""

    error_hints: ClassVar[dict[str, str]] = {}
    """依 ERROR_RULES 的名稱附加的提示文字（例如建議換哪個環境變數）。"""

    def __init__(
        self,
        *,
        model: str,
        api_key: str = "",
        base_url: str = "",
        name: str = "",
        timeout: float = 60.0,
        level: str = "",
        extra_params: dict | None = None,
    ):
        """每家都要的基本欄位收在這裡；不是每家都有的當選填。

        model      送給 API 的模型 id。
        api_key    金鑰。本機登入的（Codex）沒有。
        base_url   端點。只有一個固定端點的（Gemini）沒有。
        name       給人看的顯示名稱，跟 model 刻意分開；沒給就用 model。
        timeout    單次呼叫逾時秒數。
        level      思考／推理程度，各家叫法不同（thinking_level / reasoning_effort）。
        extra_params  原樣併進每次請求的額外參數，處理各家的怪癖。
        """
        self._model = model
        self._api_key = api_key
        self._base_url = base_url
        self.name = name or model
        self._timeout = timeout
        self._level = level
        self._extra_params = dict(extra_params or {})
        # 第一次 _connect 才建 client：組 prompt、解析回覆都不需要 SDK，
        # 測試沒裝 SDK 也能建出 adapter。
        self._client: Any = None

    def _connect(self) -> Any:
        if self._client is None:
            self._client = self._make_client()
        return self._client

    @abstractmethod
    def _make_client(self) -> Any:
        """延遲 import SDK 並建 client。只會被叫一次。"""

    # ---- 回合骨架 ----

    def complete(
        self,
        *,
        system: str,
        messages: Sequence[Message],
        tools: Sequence[ToolSpec] = (),
        schema: dict | None = None,
        max_tokens: int = 4096,
    ) -> Reply:
        request = self.build_request(
            system=system,
            messages=messages,
            tools=tools,
            schema=schema,
            max_tokens=max_tokens,
        )
        try:
            payload = self._send(request)
        except LLMError:
            raise  # adapter 自己判定的錯誤，原樣往上丟
        except Exception as error:  # SDK 的例外型別不准外洩
            dump_failed_request(request)
            raise self.classify_error(error) from error
        return self.parse_reply(payload)

    def stream(
        self,
        *,
        system: str,
        messages: Sequence[Message],
        tools: Sequence[ToolSpec] = (),
        schema: dict | None = None,
        max_tokens: int = 4096,
    ) -> Iterator[StreamChunk]:
        """預設：不是真的串流，墊一個一次性 chunk。真的能串的 adapter 覆寫這個。"""
        yield from complete_as_stream(
            self.complete,
            system=system,
            messages=messages,
            tools=tools,
            schema=schema,
            max_tokens=max_tokens,
        )

    @abstractmethod
    def build_request(
        self,
        *,
        system: str,
        messages: Sequence[Message],
        tools: Sequence[ToolSpec],
        schema: dict | None,
        max_tokens: int,
    ) -> dict:
        """組出送給 SDK 的參數。用 build_input / build_tools 組內容，自己排鍵。"""

    @abstractmethod
    def _send(self, request: dict) -> Any:
        """真的呼叫 SDK，回傳純資料（dict）的 payload，交給 parse_reply。"""

    def classify_error(self, error: Exception) -> LLMError:
        return classify_error(
            error, provider=self.provider, timeout=self._timeout, hints=self.error_hints
        )

    # ---- prompt 組裝：共用的走訪，每個 role 的字面值交給 hook ----

    def build_input(self, messages: Sequence[Message]) -> list[Step]:
        """把中性訊息串攤平成 wire format 的訊息陣列。

        模型那一手一律**原樣回放** `provider_steps`，不自己重建。Gemini 3.x 的
        function_call 帶著一個 thought 簽章，少了它工具結果會被拒；自己拼一個
        function_call 湊不出那個簽章，所以只能保留原件。沒有 provider_steps
        的（從資料庫讀回的歷史）才交給 format_assistant 用文字重建。
        """
        steps: list[Step] = []
        for message in messages:
            if message.role == "user":
                steps.append(self.format_user(message))
            elif message.role == "assistant":
                if message.provider_steps:
                    steps.extend(message.provider_steps)
                else:
                    steps.extend(self.format_assistant(message))
            elif message.role == "tool":
                steps.append(self.format_tool_result(message))
            else:
                raise LLMError(f"未知的訊息 role: {message.role}")
        return steps

    # 四個 format hook 的預設是 OpenAI Chat Completions 的形狀：相容端點都講這個，
    # 是 de-facto 的 wire format，所以當基本建設。走別種格式的（Gemini）自己覆寫。

    def format_user(self, message: Message) -> Step:
        return {"role": "user", "content": message.content}

    def format_assistant(self, message: Message) -> list[Step]:
        """只在沒有 provider_steps 可回放時被叫到。沒東西可重建就回空 list。

        Chat Completions 要求 tool 結果前面要有帶同樣 id 的 assistant tool_calls，
        而且沒有簽章這種東西，所以模型那一手直接從中性型別重建。
        """
        if not message.content and not message.tool_calls:
            return []
        step: dict[str, Any] = {"role": "assistant", "content": message.content or None}
        if message.tool_calls:
            step["tool_calls"] = [
                {
                    "id": call.id,
                    "type": "function",
                    "function": {
                        "name": call.name,
                        "arguments": json.dumps(call.arguments, ensure_ascii=False),
                    },
                }
                for call in message.tool_calls
            ]
        return [step]

    def format_tool_result(self, message: Message) -> Step:
        return {
            "role": "tool",
            "tool_call_id": message.tool_call_id,
            "content": message.content,
        }

    # ---- 工具組裝 ----

    def build_tools(self, tools: Sequence[ToolSpec]) -> list[Step]:
        return [self.format_tool(tool) for tool in tools]

    def format_tool(self, tool: ToolSpec) -> Step:
        return {"type": "function", "function": function_declaration(tool)}

    # ---- 回覆處理：共用的組 Reply，從 payload 撈欄位交給 hook ----

    def parse_reply(self, payload: Any) -> Reply:
        failure = self.failure_of(payload)
        if failure is not None:
            raise LLMError(f"{self.provider} 回覆失敗: {failure}")
        tool_calls = tuple(
            ToolCall(id=call_id or "", name=name or "", arguments=parse_arguments(raw))
            for call_id, name, raw in self.raw_tool_calls(payload)
        )
        return Reply(
            text=self.text_of(payload),
            tool_calls=tool_calls,
            stop_reason=resolve_stop_reason(
                tool_calls=tool_calls,
                truncated=self.is_truncated(payload),
                refused=self.is_refused(payload),
            ),
            usage=self.usage_of(payload),
            provider_steps=self.replayable_steps(payload),
        )

    @abstractmethod
    def text_of(self, payload: Any) -> str: ...

    @abstractmethod
    def raw_tool_calls(self, payload: Any) -> Iterable[tuple[str, str, Any]]:
        """每個 tool call 的 (id, name, 未解析的 arguments)。解析由 parse_reply 做。"""

    @abstractmethod
    def usage_of(self, payload: Any) -> Usage: ...

    def failure_of(self, payload: Any) -> Any | None:
        """回非 None 代表整輪失敗，內容會進錯誤訊息。"""
        return None

    def is_truncated(self, payload: Any) -> bool:
        """撞到 max_tokens 被切斷。"""
        return False

    def is_refused(self, payload: Any) -> bool:
        """內容被供應商拒絕。"""
        return False

    def replayable_steps(self, payload: Any) -> tuple:
        """續跑時要原樣送回的步驟。見 Message.provider_steps。"""
        return ()
