"""OpenAI 相容端點的 adapter（Chat Completions）。

任何講 OpenAI Chat Completions 的端點都用這一個 class：NVIDIA NIM、Ollama、
Groq、Gemini 的相容端點……差別只在 url、token 與模型 id，這些寫在
`llm/config/*.json` 裡，由 `model_factory(model_id)` 讀出來建物件。
新增模型 = 改 JSON，不用寫程式。

openai SDK 的型別只准出現在這個檔案裡。走訪、回放、組 Reply、錯誤分類、
Chat Completions 形狀的 format hook 都在 llm.base；這裡只剩 request 的鍵、
真的送出去、以及從回覆撈欄位。

設定檔格式（見 config/README.md）：`providers` 是端點清單（name / url / token），
`models` 是模型清單，每個模型用 `provider` 指回它的端點。模型的 `id` 是送給
API 的名字也是查詢 key，`display_name` 是給人看的名字，兩者刻意分開：改顯示
名稱永遠不會動到請求。
"""

import json
from pathlib import Path
from typing import Any, Iterable, Sequence

from .base import BaseLLM, Step
from .types import LLMError, Message, ToolSpec, Usage

CONFIG_DIR = Path(__file__).resolve().parent / "config"
DEFAULT_TIMEOUT_SECONDS = 60.0


# ---- 設定檔 ----


def load_config() -> dict:
    """合併目錄裡所有 *.json：`providers` 與 `models` 兩個 list 各自串接。

    丟一個新檔案進去就會被讀到；不需要登錄。
    """
    merged: dict = {"providers": [], "models": []}
    for path in sorted(CONFIG_DIR.glob("*.json")):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except ValueError as error:
            raise LLMError(f"模型設定檔 {path.name} 不是合法 JSON: {error}") from error
        if not isinstance(data, dict):
            raise LLMError(f"模型設定檔 {path.name} 最上層必須是物件")
        for key in merged:
            merged[key].extend(data.get(key) or [])
    return merged


def list_models() -> list[dict]:
    """給 /v1/models 用的清單。只有 id 與顯示名稱，絕不含 token。"""
    return [
        {"id": m["id"], "display_name": m.get("display_name") or m["id"]}
        for m in load_config()["models"]
    ]


def model_factory(model_id: str) -> "OpenAICompatibleLLM":
    """依 model id 從設定檔建出 adapter；url / token 從模型指的 provider 拿。"""
    config = load_config()
    m = next((m for m in config["models"] if m.get("id") == model_id), None)
    if m is None:
        raise LLMError(f"設定檔沒有模型 {model_id}")
    p = next((p for p in config["providers"] if p.get("name") == m.get("provider")), None)
    if p is None:
        raise LLMError(f"模型 {model_id} 指的 provider {m.get('provider')!r} 不存在")
    return OpenAICompatibleLLM(
        api_key=p["token"],
        base_url=p["url"],
        model=m["id"],
        name=m.get("display_name", m["id"]),
        timeout=m.get("timeout", DEFAULT_TIMEOUT_SECONDS),
        level=m.get("level", ""),
        extra_params=m.get("ex_params"),
    )


# ---- adapter ----


def _choice(payload: dict) -> dict:
    choices = payload.get("choices") or []
    return choices[0] if choices and isinstance(choices[0], dict) else {}


def _message(payload: dict) -> dict:
    return _choice(payload).get("message") or {}


class OpenAICompatibleLLM(BaseLLM):
    """單獨可用：給 api_key、base_url、model 就能跑。建構子與 format hook 全用 BaseLLM 的。"""

    provider = "OpenAI"

    def _make_client(self):
        from openai import OpenAI  # 延遲載入，沒裝 openai 的環境仍可 import 其他 adapter

        return OpenAI(api_key=self._api_key, base_url=self._base_url, timeout=self._timeout)

    # ---- request ----

    def build_request(
        self,
        *,
        system: str,
        messages: Sequence[Message],
        tools: Sequence[ToolSpec] = (),
        schema: dict | None = None,
        max_tokens: int = 4096,
    ) -> dict:
        """組出 chat.completions.create() 的關鍵字參數。"""
        wire_messages: list[Step] = []
        if system:
            wire_messages.append({"role": "system", "content": system})
        wire_messages.extend(self.build_input(messages))

        request: dict[str, Any] = {
            "model": self._model,
            "messages": wire_messages,
            # 相容端點大多只認 max_tokens；官方的 max_completion_tokens 反而不是每家都收。
            "max_tokens": max_tokens,
        }
        if tools:
            request["tools"] = self.build_tools(tools)
        if schema is not None:
            request["response_format"] = {
                "type": "json_schema",
                "json_schema": {"name": "reply", "schema": schema},
            }
        if self._level:
            request["reasoning_effort"] = self._level
        request.update(self._extra_params)
        return request

    def _send(self, request: dict) -> dict:
        completion = self._connect().chat.completions.create(**request)
        return completion.model_dump()

    # ---- reply ----

    def failure_of(self, payload: dict) -> Any | None:
        # 有些相容端點會用 200 包一個 error 物件回來，而不是丟 HTTP 錯誤。
        if payload.get("error"):
            return payload["error"]
        if not payload.get("choices"):
            return "回覆沒有 choices"
        return None

    def text_of(self, payload: dict) -> str:
        return _message(payload).get("content") or ""

    def raw_tool_calls(self, payload: dict) -> Iterable[tuple[str, str, Any]]:
        calls = []
        for call in _message(payload).get("tool_calls") or []:
            if not isinstance(call, dict):
                continue
            function = call.get("function") or {}
            calls.append((call.get("id"), function.get("name"), function.get("arguments")))
        return calls

    def usage_of(self, payload: dict) -> Usage:
        usage = payload.get("usage") or {}
        details = usage.get("completion_tokens_details") or {}
        return Usage(
            input_tokens=usage.get("prompt_tokens") or 0,
            output_tokens=usage.get("completion_tokens") or 0,
            thought_tokens=details.get("reasoning_tokens") or 0,
        )

    def is_truncated(self, payload: dict) -> bool:
        return _choice(payload).get("finish_reason") == "length"

    def is_refused(self, payload: dict) -> bool:
        return _choice(payload).get("finish_reason") == "content_filter"
