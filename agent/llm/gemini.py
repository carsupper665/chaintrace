"""Gemini adapter（google-genai Interactions API）。

google.genai 的型別只准出現在這個檔案裡。進來出去都是 llm.types 的中性型別。
走訪、回放、組 Reply、錯誤分類都在 llm.base；這裡只有 Gemini 的欄位字面值。

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

from typing import Any, Iterable, Sequence

from .base import BaseLLM, Step, function_declaration
from .types import Message, ToolSpec, Usage

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

# 模型那一手裡必須原樣回放的步驟。thought 帶著 function_call 的簽章，
# 少了它 Gemini 會拒收工具結果（400: missing thought_signature）。
_REPLAYED_STEP_TYPES = ("thought", "function_call", "model_output")


def _text_content(text: str) -> list[dict]:
    return [{"type": "text", "text": text}]


def _steps(payload: dict) -> list[dict]:
    return [step for step in payload.get("steps") or [] if isinstance(step, dict)]


class GeminiLLM(BaseLLM):
    """單獨可用：給 api_key 就能跑，不依賴 agent 的其他任何東西。"""

    provider = "Gemini"
    error_hints = {
        "timeout": "，或把 LLM_THINKING_LEVEL 降到 low",
        "rate_limited": "（額度按模型分開計算，換模型可繼續）",
        "overloaded": "，換 LLM_MODEL（例如 gemini-3.6-flash）比等待有效",
    }

    def __init__(
        self,
        *,
        api_key: str,
        model: str = DEFAULT_MODEL,
        timeout: float = DEFAULT_TIMEOUT_SECONDS,
        thinking_level: str = DEFAULT_THINKING_LEVEL,
    ):
        super().__init__(model=model, api_key=api_key, timeout=timeout, level=thinking_level)

    def _make_client(self):
        from google import genai  # 延遲載入，沒裝 google-genai 的環境仍可 import 其他 adapter

        # HttpOptions.timeout 的單位是毫秒，跟 create(timeout=) 的秒不同，別搞混。
        return genai.Client(
            api_key=self._api_key, http_options={"timeout": int(self._timeout * 1000)}
        )

    # ---- request ----

    def format_user(self, message: Message) -> Step:
        return {"type": "user_input", "content": _text_content(message.content)}

    def format_assistant(self, message: Message) -> list[Step]:
        # 從資料庫讀回的歷史沒有簽章，只剩文字；連文字都沒有就什麼都不補。
        if not message.content:
            return []
        return [{"type": "model_output", "content": _text_content(message.content)}]

    def format_tool_result(self, message: Message) -> Step:
        step: dict[str, Any] = {
            "type": "function_result",
            "call_id": message.tool_call_id,
            "result": _text_content(message.content),
        }
        if message.name:
            step["name"] = message.name
        if message.is_error:
            step["is_error"] = True
        return step

    def format_tool(self, tool: ToolSpec) -> Step:
        # Interactions API 的工具宣告是扁平的，不像 Chat Completions 多包一層 function。
        return {"type": "function", **function_declaration(tool)}

    def build_request(
        self,
        *,
        system: str,
        messages: Sequence[Message],
        tools: Sequence[ToolSpec] = (),
        schema: dict | None = None,
        max_tokens: int = 4096,
    ) -> dict:
        """組出 interactions.create() 的關鍵字參數。"""
        request: dict[str, Any] = {
            "model": self._model,
            "input": self.build_input(messages),
            # 一律無狀態：對話歷史的 source of truth 在 Go 的資料庫，
            # 不依賴 Google 端的 previous_interaction_id（見 docs/development-rules.md 第 3 節）。
            "store": False,
            "generation_config": {
                "max_output_tokens": max_tokens,
                "thinking_level": self._level,
            },
        }
        if system:
            request["system_instruction"] = system
        if tools:
            request["tools"] = self.build_tools(tools)
        if schema is not None:
            request["response_format"] = {
                "type": "text",
                "mime_type": "application/json",
                "schema": schema,
            }
        return request

    def _send(self, request: dict) -> dict:
        # 回 model_dump() 的 dict 而非 SDK 物件，錄下來的 JSON 才能直接餵進來當測試素材。
        interaction = self._connect().interactions.create(timeout=self._timeout, **request)
        return interaction.model_dump()

    # ---- reply ----

    def failure_of(self, payload: dict) -> Any | None:
        if payload.get("status") != "failed":
            return None
        return payload.get("errors") or payload.get("error")

    def text_of(self, payload: dict) -> str:
        return payload.get("output_text") or ""

    def raw_tool_calls(self, payload: dict) -> Iterable[tuple[str, str, Any]]:
        return [
            (step.get("id"), step.get("name"), step.get("arguments"))
            for step in _steps(payload)
            if step.get("type") == "function_call"
        ]

    def usage_of(self, payload: dict) -> Usage:
        usage = payload.get("usage") or {}
        return Usage(
            input_tokens=usage.get("total_input_tokens") or 0,
            output_tokens=usage.get("total_output_tokens") or 0,
            thought_tokens=usage.get("total_thought_tokens") or 0,
        )

    def is_truncated(self, payload: dict) -> bool:
        # 撞到 max_output_tokens 就被切斷，status 是 "incomplete" 而不是 "failed"。
        return payload.get("status") == "incomplete"

    def replayable_steps(self, payload: dict) -> tuple:
        return tuple(step for step in _steps(payload) if step.get("type") in _REPLAYED_STEP_TYPES)
