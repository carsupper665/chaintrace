"""真的呼叫一次設定中的 LLM provider，確認連線與 adapter 都通。

這不是自動化測試——它要網路、要金鑰、會花額度。自動化測試一律用 FakeLLM。

    .venv/Scripts/python.exe smoke.py
"""

import os
import sys
import time

from app import load_env
from llm import LLMError, from_env
from llm.types import Message, ToolSpec
from prompt import build_system_prompt

EVIDENCE = {
    "investigation": {
        "id": "inv_smoke",
        "title": "Smoke test",
        "address": "TSmokeTestAddress0000000000000000001",
        "network": "TRON_MAINNET",
        "status": "已完成",
        "targetLocked": True,
    },
    "activeRun": None,
    "analysis": {
        "dataset": {
            "id": "ds_smoke",
            "network": "TRON_MAINNET",
            "asset": "USDT",
            "partial": True,
            "stopReason": "transfer_limit_reached",
            "confidence": 60,
            "transferLimit": 500,
            "traversalDepth": 2,
            "collectedTransfers": 500,
            "windowStart": "2026-07-30T00:00:00Z",
            "windowEnd": "2026-08-29T00:00:00Z",
        },
        "metrics": {"relatedNodes": 37, "transferCount": 500},
        "assessment": {
            "score": 60,
            "level": "high",
            "reasons": ["fan_out", "rapid_forwarding"],
            "nodeAssessments": [
                {
                    "address": "TSmokeCounterparty000000000000000002",
                    "score": 45,
                    "level": "medium",
                    "reasons": ["fan_out"],
                }
            ],
        },
        "ruleExamples": {
            "fan_out": [
                {
                    "address": "TSmokeTestAddress0000000000000000001",
                    "transactionHash": "0xsmoke1",
                    "note": "轉出給 14 個不同地址",
                }
            ],
            "rapid_forwarding": [
                {
                    "address": "TSmokeCounterparty000000000000000002",
                    "transactionHash": "0xsmoke2",
                    "note": "收款後 12 分鐘轉出 92%",
                }
            ],
        },
    },
}

# 全新空白調查：沒有地址、沒有分析。用來驗證 agent 知道要先開始調查。
BLANK_EVIDENCE = {
    "investigation": {
        "id": "inv_blank",
        "title": "新調查任務",
        "address": None,
        "network": "TRON_MAINNET",
        "status": "待處理",
        "targetLocked": False,
    },
    "activeRun": None,
    "analysis": None,
}

def _address_schema(description: str) -> dict:
    return {
        "type": "object",
        "properties": {"address": {"type": "string", "description": description}},
        "required": ["address"],
        "additionalProperties": False,
    }


# 跟 agentclient/toolspec.go 的 ToolSpecs() 對齊。
TOOLS = [
    ToolSpec(
        name="expand_node",
        description="展開某個地址在本次分析範圍內的資金往來關係。",
        parameters=_address_schema("要展開的地址"),
    ),
    ToolSpec(
        name="set_investigation_target",
        description="設定這筆調查要追查的地址。只有在調查還沒鎖定目標時可用。",
        parameters=_address_schema("要追查的 TRON Base58Check 地址（T 開頭）。"),
    ),
    ToolSpec(
        name="start_analysis",
        description=(
            "對目前的調查目標啟動一次鏈上資料蒐集與風險評估。需要先設定好目標。"
            "這是非同步的：呼叫後會立刻回傳，蒐集要跑數分鐘，"
            "**不要等它完成，也不要重複呼叫**，直接告訴使用者已經開始。"
        ),
        parameters={"type": "object", "properties": {}, "additionalProperties": False},
    ),
]


def check_ping(llm) -> None:
    """最小請求。這一步失敗就是網路或金鑰的問題，跟我們的 prompt 無關。"""
    print("=== 0. 連線測試（最小請求）===")
    started = time.monotonic()
    reply = llm.complete(
        system="只回覆 OK 兩個字。",
        messages=[Message("user", "ping")],
        # thinking 模型會先吃掉一部分預算，給 16 的話答案沒空間可寫。
        max_tokens=500,
    )
    print(f"  回覆: {reply.text.strip()!r}  ({time.monotonic() - started:.1f} 秒)")


def check_summary(llm) -> str:
    print("\n=== 1. 摘要模式（無工具）===")
    started = time.monotonic()
    reply = llm.complete(
        system=build_system_prompt("summary", EVIDENCE),
        messages=[Message("user", "請為這份調查產生摘要。")],
        # 預算含思考：low 大約吃 1000，抓 3000 才有空間寫完 400 字。
        max_tokens=3000,
    )
    elapsed = time.monotonic() - started
    print(reply.text)
    print(
        f"\n[{elapsed:.1f} 秒 stop_reason={reply.stop_reason} "
        f"tokens in={reply.usage.input_tokens} out={reply.usage.output_tokens} "
        f"thought={reply.usage.thought_tokens}]"
    )
    if reply.stop_reason == "length":
        print("  !! 回覆被切斷了，要調高 LLM_MAX_TOKENS")
    return reply.text


def check_tool_call(llm) -> None:
    print("\n=== 2. 問答模式（給工具，看它會不會叫）===")
    started = time.monotonic()
    reply = llm.complete(
        system=build_system_prompt("chat", EVIDENCE),
        messages=[
            Message("user", "TSmokeCounterparty000000000000000002 這個地址的往來對象有哪些？")
        ],
        tools=TOOLS,
        max_tokens=3000,
    )
    print(f"  ({time.monotonic() - started:.1f} 秒)")
    if reply.tool_calls:
        for call in reply.tool_calls:
            print(f"  呼叫工具 {call.name}({call.arguments})  id={call.id}")
    else:
        print("  沒有呼叫工具，直接回答：")
        print(" ", reply.text[:400])
    print(f"\n[stop_reason={reply.stop_reason}]")


def check_blank_investigation(llm) -> None:
    """全新空白調查 + 一個地址：agent 應該自己去設目標並啟動分析。"""
    print("\n=== 3. 空白調查（agent 該自己開始調查）===")
    started = time.monotonic()
    reply = llm.complete(
        system=build_system_prompt("chat", BLANK_EVIDENCE),
        messages=[
            Message("user", "幫我調查 TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t")
        ],
        tools=TOOLS,
        max_tokens=3000,
    )
    print(f"  ({time.monotonic() - started:.1f} 秒)")
    names = [call.name for call in reply.tool_calls]
    for call in reply.tool_calls:
        print(f"  呼叫工具 {call.name}({call.arguments})")
    if not names:
        print("  沒有呼叫任何工具，直接回答：")
        print(" ", reply.text[:300])
    print(f"  設定目標： {'是' if 'set_investigation_target' in names else '否 ← 要檢查 prompt'}")
    print(f"  沒有亂查資料： {'是' if 'expand_node' not in names else '否 ← 它在沒資料時就想查'}")


def explain_ping_failure(error: LLMError) -> None:
    if os.getenv("LLM_PROVIDER", "gemini").strip().lower() == "codex":
        print("\nCodex 最小請求失敗。檢查：")
        print("  1. `codex login` 是否已完成")
        print("  2. CODEX_COMMAND 是否能啟動 app-server")
        print("  3. 這台機器是否能連 api.openai.com")
        return
    if error.retryable:
        # 連得上但沒回應：網路或服務端的問題。
        print("\n最小請求都連不上。檢查：")
        print("  1. 這台機器能不能連 generativelanguage.googleapis.com（VPN／防火牆／Proxy）")
        print("  2. 稍後再試一次，可能是暫時性的")
        return
    # 服務端明確拒絕：是我們的設定寫錯，重試沒用。
    print("\nGemini 明確拒絕了這個請求，重試沒用。檢查 agent/.env：")
    print("  LLM_API_KEY          去 https://aistudio.google.com/apikey 對一下")
    print("  LLM_MODEL            實測可用：gemini-3.6-flash、gemini-3.5-flash-lite")
    print("  LLM_THINKING_LEVEL   只收 low / medium / high（minimal 會被拒）")


def check_grounding(summary: str) -> None:
    """證據的 partial 是 true，摘要必須誠實聲明範圍不完整並引用得出證據。"""
    print("\n=== 4. 規則自我檢查 ===")
    honest = any(word in summary for word in ("不完整", "未涵蓋", "上限", "部分"))
    cited = "TSmoke" in summary or "0xsmoke" in summary
    print(f"  有聲明分析範圍不完整： {'是' if honest else '否 ← 要檢查 prompt'}")
    print(f"  有引用具體地址或 hash： {'是' if cited else '否 ← 要檢查 prompt'}")


def main() -> int:
    load_env()
    try:
        llm = from_env()
    except LLMError as error:
        print(f"建不出 adapter：{error}")
        print("請確認 agent/.env 裡的 provider 設定與憑證。")
        return 1

    print(
        f"model={os.getenv('LLM_MODEL') or '(預設)'}  "
        f"timeout={os.getenv('LLM_TIMEOUT_SECONDS') or '120'}s  "
        f"thinking={os.getenv('LLM_REASONING_EFFORT') or os.getenv('LLM_THINKING_LEVEL') or 'default'}\n"
    )

    try:
        check_ping(llm)
    except LLMError as error:
        print(f"  失敗：{error}")
        explain_ping_failure(error)
        return 1

    try:
        summary = check_summary(llm)
        check_tool_call(llm)
        check_blank_investigation(llm)
    except LLMError as error:
        print(f"\n呼叫失敗：{error}")
        print("\n連線測試有過，代表金鑰與網路沒問題，是這個請求本身的問題。試試：")
        print("  換 LLM_MODEL（gemini-3.6-flash / gemini-3.5-flash-lite）")
        print("  或把 LLM_TIMEOUT_SECONDS 調到 300")
        return 1

    check_grounding(summary)
    return 0


if __name__ == "__main__":
    sys.exit(main())
