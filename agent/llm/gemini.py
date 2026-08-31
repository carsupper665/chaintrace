"""Gemini adapter（google-genai Interactions API）。

google.genai 的型別只准出現在這個檔案裡。進來出去都是 llm.types 的中性型別。

欄位形狀是對 google-genai 2.20.0 的實際型別核對過的，不是憑印象寫的：
  UserInputStep     type="user_input"     content=[TextContent]
  ModelOutputStep   type="model_output"   content=[TextContent]
  FunctionCallStep  type="function_call"  id / name / arguments(dict)
  FunctionResultStep type="function_result" call_id / name / result / is_error
  Function(tool)    type="function"       name / description / parameters
  TextResponseFormat type="text"          mime_type / schema
  Usage             total_input_tokens / total_output_tokens / total_thought_tokens

實測踩到的兩個坑（2026-08-30）：
  1. max_output_tokens **包含思考的 token**。thinking_level=low 大約會吃掉 1000，
     所以預算要抓「思考 + 答案」，不是只抓答案。
  2. 撞到預算時 status 是 "incomplete"，不是 "failed"，output_text 會是半截的。
     必須翻成 stop_reason="length"，不能當成正常結束。
"""

import json
from typing import Any, Sequence

from .types import LLMError, Message, Reply, StopReason, ToolCall, ToolSpec, Usage

# 實測 2026-08-30：gemini-3.7-flash 伺服器端持續過載（60 秒不回），
# gemini-3.6-flash 1.6 秒回應。用會動的那個。
DEFAULT_MODEL = "gemini-3.6-flash"
DEFAULT_TIMEOUT_SECONDS = 120.0

# Gemini 3.x 預設會先思考再回答，而且無法完全關閉。我們的任務是把已經算好的
# 結構化證據講成人話，不需要深度推理，所以壓到 low：省時間、省 token。
#
# gemini-3.7-flash 只接受 low / medium / high。SDK 的型別另外列了 minimal，
# 但這個模型會回 400 拒絕它——型別能過不代表模型收。
DEFAULT_THINKING_LEVEL = "low"


def _text_content(text: str) -> list[dict]:
    return [{"type": "text", "text": text}]


def build_input(messages: Sequence[Message]) -> list[dict]:
    """把中性訊息串攤平成 Interactions API 的 step 陣列。

    模型那一手一律**原樣回放** `provider_steps`，不自己重建。Gemini 3.x 的
    function_call 帶著一個 thought 簽章，少了它工具結果會被拒：
    `400 Function call is missing a thought_signature in functionCall parts`。
    自己拼一個 function_call step 湊不出那個簽章，所以只能保留原件。
    """
    steps: list[dict] = []
    for message in messages:
        if message.role == "user":
            steps.append({"type": "user_input", "content": _text_content(message.content)})
        elif message.role == "assistant":
            if message.provider_steps:
                steps.extend(message.provider_steps)
            elif message.content:
                # 從資料庫讀回的歷史沒有簽章，只剩文字。
                steps.append({"type": "model_output", "content": _text_content(message.content)})
        elif message.role == "tool":
            step: dict[str, Any] = {
                "type": "function_result",
                "call_id": message.tool_call_id,
                "result": _text_content(message.content),
            }
            if message.name:
                step["name"] = message.name
            if message.is_error:
                step["is_error"] = True
            steps.append(step)
        else:
            raise LLMError(f"未知的訊息 role: {message.role}")
    return steps


def build_request(
    *,
    model: str,
    system: str,
    messages: Sequence[Message],
    tools: Sequence[ToolSpec] = (),
    schema: dict | None = None,
    max_tokens: int = 4096,
    thinking_level: str = DEFAULT_THINKING_LEVEL,
) -> dict:
    """組出 interactions.create() 的關鍵字參數。純函式，方便單獨測試。"""
    request: dict[str, Any] = {
        "model": model,
        "input": build_input(messages),
        # 一律無狀態：對話歷史的 source of truth 在 Go 的資料庫，
        # 不依賴 Google 端的 previous_interaction_id（見 docs/development-rules.md 第 3 節）。
        "store": False,
        "generation_config": {
            "max_output_tokens": max_tokens,
            "thinking_level": thinking_level,
        },
    }
    if system:
        request["system_instruction"] = system
    if tools:
        request["tools"] = [
            {
                "type": "function",
                "name": tool.name,
                "description": tool.description,
                "parameters": tool.parameters,
            }
            for tool in tools
        ]
    if schema is not None:
        request["response_format"] = {
            "type": "text",
            "mime_type": "application/json",
            "schema": schema,
        }
    return request


def _parse_arguments(raw) -> dict:
    """文件與型別都說 arguments 是 dict，但收到字串時寧可解析也不要整輪炸掉。"""
    if not isinstance(raw, str):
        return raw or {}
    try:
        return json.loads(raw)
    except ValueError as error:
        raise LLMError(f"function_call 參數不是合法 JSON: {error}") from error


# 模型那一手裡必須原樣回放的步驟。thought 帶著 function_call 的簽章，
# 少了它 Gemini 會拒收工具結果。
_REPLAYED_STEP_TYPES = ("thought", "function_call", "model_output")


def _replayable_steps(payload: dict) -> tuple:
    steps = payload.get("steps") or []
    return tuple(
        step for step in steps
        if isinstance(step, dict) and step.get("type") in _REPLAYED_STEP_TYPES
    )


def _tool_calls_from(payload: dict) -> tuple[ToolCall, ...]:
    steps = payload.get("steps") or []
    return tuple(
        ToolCall(
            id=step.get("id") or "",
            name=step.get("name") or "",
            arguments=_parse_arguments(step.get("arguments")),
        )
        for step in steps
        if isinstance(step, dict) and step.get("type") == "function_call"
    )


def _usage_from(payload: dict) -> Usage:
    usage = payload.get("usage") or {}
    return Usage(
        input_tokens=usage.get("total_input_tokens") or 0,
        output_tokens=usage.get("total_output_tokens") or 0,
        thought_tokens=usage.get("total_thought_tokens") or 0,
    )


def _stop_reason_from(status, tool_calls: tuple[ToolCall, ...]) -> StopReason:
    if tool_calls:
        return "tool_calls"
    if status == "incomplete":
        # 撞到 max_output_tokens 就被切斷。絕對不能當成正常結束回報——
        # 上層會把半截的摘要當完整的存進資料庫。
        return "length"
    return "stop"


def parse_reply(payload: dict) -> Reply:
    """把 Interaction 的 model_dump() 翻成中性 Reply。

    吃 dict 而非 SDK 物件，錄下來的 JSON 才能直接餵進來當測試素材。
    """
    status = payload.get("status")
    if status == "failed":
        errors = payload.get("errors") or payload.get("error")
        raise LLMError(f"Gemini interaction failed: {errors}")

    tool_calls = _tool_calls_from(payload)
    return Reply(
        text=payload.get("output_text") or "",
        tool_calls=tool_calls,
        stop_reason=_stop_reason_from(status, tool_calls),
        usage=_usage_from(payload),
        provider_steps=_replayable_steps(payload),
    )


# 錯誤分類表：(訊息特徵, 是否值得重試, 說明)。
# retryable 決定上層要不要重試，所以 400 這種請求本身有問題的絕不能標成可重試——
# 重試一百次還是同樣的 400。
_ERROR_RULES: list[tuple[tuple[str, ...], bool, "object"]] = [
    (
        ("timed out", "timeout"),
        True,
        lambda text, timeout: (
            f"Gemini 在 {timeout:.0f} 秒內沒有回應。"
            "可調高 LLM_TIMEOUT_SECONDS，或把 LLM_THINKING_LEVEL 降到 low。"
        ),
    ),
    (
        ("error code: 400", "invalid_request"),
        False,
        lambda text, _: f"Gemini 拒絕這個請求（設定或參數有誤）：{text}",
    ),
    (
        ("error code: 401", "error code: 403"),
        False,
        lambda text, _: f"Gemini 拒絕存取，請檢查 LLM_API_KEY：{text}",
    ),
    (
        ("error code: 429",),
        True,
        lambda text, _: f"Gemini 額度或速率上限（額度按模型分開計算，換模型可繼續）：{text}",
    ),
    (
        ("high demand", "unavailable", "error code: 503"),
        True,
        # 模型過載。重試通常沒用，換一個模型比較快。
        lambda text, _: (
            f"Gemini 這個模型目前過載，換 LLM_MODEL（例如 gemini-3.6-flash）比等待有效：{text}"
        ),
    ),
]


def _dump_failed_request(request: dict) -> None:
    """把被拒絕的請求寫到 AGENT_DEBUG_DUMP 指定的檔案。

    Gemini 對格式問題只回一句「Request contains an invalid argument.」，不指出
    哪裡錯，所以沒有原始 payload 就只能一路猜。預設關閉：payload 含證據內容。
    """
    import os

    path = os.getenv("AGENT_DEBUG_DUMP", "").strip()
    if not path:
        return
    try:
        with open(path, "w", encoding="utf-8") as handle:
            json.dump(request, handle, ensure_ascii=False, indent=1, default=str)
    except OSError:
        pass  # 診斷用的旁支，絕不能因此讓真正的錯誤消失


def _classify(error: Exception, timeout: float) -> LLMError:
    """把 SDK 例外分成「我們寫錯」和「等一下再試」。

    retryable 決定上層要不要重試，所以 400 這種請求本身有問題的絕不能標成可重試——
    重試一百次還是同樣的 400。
    """
    text = str(error)
    lowered = text.lower()
    for markers, retryable, explain in _ERROR_RULES:
        if any(marker in lowered for marker in markers):
            return LLMError(explain(text, timeout), retryable=retryable)
    return LLMError(f"Gemini 呼叫失敗: {text}", retryable=True)


class GeminiLLM:
    """單獨可用：給 api_key 就能跑，不依賴 agent 的其他任何東西。"""

    def __init__(
        self,
        *,
        api_key: str,
        model: str = DEFAULT_MODEL,
        timeout: float = DEFAULT_TIMEOUT_SECONDS,
        thinking_level: str = DEFAULT_THINKING_LEVEL,
    ):
        from google import genai  # 延遲載入，沒裝 google-genai 的環境仍可 import 其他 adapter

        # HttpOptions.timeout 的單位是毫秒，跟 create(timeout=) 的秒不同，別搞混。
        self._client = genai.Client(
            api_key=api_key, http_options={"timeout": int(timeout * 1000)}
        )
        self._model = model
        self._timeout = timeout
        self._thinking_level = thinking_level

    def complete(
        self,
        *,
        system: str,
        messages: Sequence[Message],
        tools: Sequence[ToolSpec] = (),
        schema: dict | None = None,
        max_tokens: int = 4096,
    ) -> Reply:
        request = build_request(
            model=self._model,
            system=system,
            messages=messages,
            tools=tools,
            schema=schema,
            max_tokens=max_tokens,
            thinking_level=self._thinking_level,
        )
        try:
            interaction = self._client.interactions.create(timeout=self._timeout, **request)
        except Exception as error:  # SDK 的例外型別不准外洩
            _dump_failed_request(request)
            raise _classify(error, self._timeout) from error
        return parse_reply(interaction.model_dump())
