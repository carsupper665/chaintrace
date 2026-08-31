"""System prompt 組裝。

Prompt 存成 .md 檔而不是字串常數，才 diff 得動、review 得了。

證據放在 system prompt 而不是 user 訊息裡，因為它在整個 session 內是固定的
（dataset 不可變），擺在前面對快取友善，也讓對話歷史保持乾淨。
"""

import json
import pathlib
from functools import lru_cache
from typing import Literal

Mode = Literal["summary", "chat"]

PROMPT_DIR = pathlib.Path(__file__).resolve().parent / "prompts"
MODE_FILES: dict[str, str] = {"summary": "summary.md", "chat": "chat.md"}


class PromptError(RuntimeError):
    pass


@lru_cache(maxsize=8)
def _read(filename: str) -> str:
    path = PROMPT_DIR / filename
    try:
        text = path.read_text(encoding="utf-8").strip()
    except OSError as error:
        raise PromptError(f"讀不到 prompt 檔 {path}: {error}") from error
    if not text:
        raise PromptError(f"prompt 檔是空的: {path}")
    return text


def build_system_prompt(mode: Mode, evidence: dict) -> str:
    """組出一個 session 用的 system prompt。

    同一個 dataset 組出來的結果一樣，所以呼叫端可以連同 session 一起快取。
    """
    if mode not in MODE_FILES:
        raise PromptError(f"未知的模式: {mode}")
    evidence_json = json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True)
    return "\n\n".join(
        [
            _read("common.md"),
            _read(MODE_FILES[mode]),
            "## 證據",
            "以下是本次調查的全部可用證據。你的回答只能建立在這份資料上。",
            f"```json\n{evidence_json}\n```",
        ]
    )


def verify_prompts_present() -> None:
    """啟動時呼叫，讓缺檔在開機時就炸，而不是等到第一個請求。"""
    _read("common.md")
    for filename in MODE_FILES.values():
        _read(filename)
