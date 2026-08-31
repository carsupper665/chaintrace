"""LLM adapter。

上層只從這裡拿東西，不直接 import 各家的模組。
"""

import os

from .base import LLM
from .fake import FakeLLM
from .types import LLMError, Message, Reply, StopReason, ToolCall, ToolSpec, Usage

__all__ = [
    "LLM",
    "FakeLLM",
    "LLMError",
    "Message",
    "Reply",
    "StopReason",
    "ToolCall",
    "ToolSpec",
    "Usage",
    "from_env",
]


def _float_env(key: str, default: float) -> float:
    try:
        return float(os.getenv(key, "").strip() or default)
    except ValueError:
        return default


def from_env() -> LLM:
    """依環境變數建出 adapter。

    延遲載入，所以只裝了 Gemini 的環境不會因為 openai/anthropic 沒裝而爆掉。
    """
    provider = os.getenv("LLM_PROVIDER", "gemini").strip().lower()
    api_key = os.getenv("LLM_API_KEY", "").strip()
    model = os.getenv("LLM_MODEL", "").strip()

    if provider == "fake":
        return FakeLLM()
    if not api_key:
        raise LLMError(f"LLM_API_KEY 未設定，無法建立 {provider} adapter")
    if provider == "gemini":
        from .gemini import (
            DEFAULT_MODEL,
            DEFAULT_THINKING_LEVEL,
            DEFAULT_TIMEOUT_SECONDS,
            GeminiLLM,
        )

        return GeminiLLM(
            api_key=api_key,
            model=model or DEFAULT_MODEL,
            timeout=_float_env("LLM_TIMEOUT_SECONDS", DEFAULT_TIMEOUT_SECONDS),
            thinking_level=os.getenv("LLM_THINKING_LEVEL", "").strip() or DEFAULT_THINKING_LEVEL,
        )
    raise LLMError(f"不支援的 LLM_PROVIDER: {provider}")
