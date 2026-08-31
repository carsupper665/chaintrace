"""System prompt 組裝測試。"""

import json

import pytest

from prompt import PromptError, build_system_prompt, verify_prompts_present

EVIDENCE = {
    "investigation": {"id": "inv1", "address": None, "status": "待處理", "targetLocked": False},
    "activeRun": None,
    "analysis": {
        "dataset": {"partial": True, "stopReason": "provider_rate_limited"},
        "assessment": {"score": 60, "reasons": ["fan_out"]},
    },
}


def test_all_prompt_files_exist():
    verify_prompts_present()


def test_summary_prompt_carries_rules_mode_and_evidence():
    prompt = build_system_prompt("summary", EVIDENCE)
    assert "繁體中文" in prompt              # 來自 common.md
    assert "分析範圍：" in prompt            # 來自 summary.md
    assert "provider_rate_limited" in prompt  # 證據本身
    assert "## 證據" in prompt


def test_summary_prompt_forbids_markdown():
    # 前端逐字顯示對話內容，沒有 Markdown 渲染器，# 和 * 會原樣露出來。
    prompt = build_system_prompt("summary", EVIDENCE)
    assert "不要用 Markdown" in prompt


def test_chat_prompt_uses_the_chat_mode_file():
    prompt = build_system_prompt("chat", EVIDENCE)
    assert "out_of_scope" in prompt
    assert "分析範圍：" not in prompt


def test_chat_prompt_teaches_the_investigation_tools():
    # 沒有這些，agent 收到地址也不會知道自己能動手。
    prompt = build_system_prompt("chat", EVIDENCE)
    for tool in ("set_investigation_target", "start_analysis"):
        assert tool in prompt


def test_chat_prompt_forbids_waiting_for_a_run():
    # ADR-0014：啟動分析後這一輪就該結束，不能空轉等待。
    prompt = build_system_prompt("chat", EVIDENCE)
    assert "啟動後絕對不要" in prompt
    assert "analysis_already_running" in prompt


def test_common_prompt_explains_the_three_evidence_blocks():
    prompt = build_system_prompt("chat", EVIDENCE)
    for field in ("investigation", "activeRun", "analysis"):
        assert f"`{field}`" in prompt


def test_addresses_must_be_copied_verbatim():
    # 實測看過模型把 TSmokeCounterparty 寫成 TSmokeSmokeCounterparty。
    # 在這個領域，改一個字元就是指向另一個錢包。
    for mode in ("summary", "chat"):
        assert "逐字照抄" in build_system_prompt(mode, EVIDENCE)


def test_common_rules_are_in_both_modes():
    for mode in ("summary", "chat"):
        prompt = build_system_prompt(mode, EVIDENCE)
        # ADR-0011 的線：LLM 不能改分數、不能造證據。
        assert "Risk Score 是系統用決定性規則算出來的" in prompt
        assert "提供的資料中沒有這項資訊" in prompt


def test_every_rule_code_is_explained():
    prompt = build_system_prompt("summary", EVIDENCE)
    for code in (
        "fan_out",
        "fan_in",
        "rapid_forwarding",
        "target_connected_forwarding",
        "circular_flow",
    ):
        assert code in prompt


def test_every_stop_reason_is_explained():
    prompt = build_system_prompt("chat", EVIDENCE)
    for code in (
        "source_exhausted",
        "transfer_limit_reached",
        "traversal_depth_reached",
        "no_eligible_transfers",
        "provider_rate_limited",
        "provider_unavailable",
        "resource_limit_reached",
    ):
        assert code in prompt


def test_evidence_is_embedded_as_readable_json():
    prompt = build_system_prompt("summary", EVIDENCE)
    body = prompt.split("```json\n", 1)[1].rsplit("\n```", 1)[0]
    assert json.loads(body) == EVIDENCE


def test_same_evidence_gives_the_same_prompt():
    assert build_system_prompt("chat", EVIDENCE) == build_system_prompt("chat", dict(EVIDENCE))


def test_unknown_mode_is_rejected():
    with pytest.raises(PromptError):
        build_system_prompt("nope", EVIDENCE)  # type: ignore[arg-type]
