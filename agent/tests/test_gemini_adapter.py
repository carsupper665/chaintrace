"""Gemini adapter 的翻譯測試。

素材是用 google-genai 自己的 pydantic 型別產生的，所以欄位名由 SDK 背書。
這些測試不碰網路、不需要金鑰。
"""

import json
import pathlib

import pytest

from llm.gemini import build_input, build_request, parse_reply
from llm.types import LLMError, Message, ToolCall, ToolSpec

TESTDATA = pathlib.Path(__file__).resolve().parent.parent / "llm" / "testdata" / "gemini"


def load(name: str) -> dict:
    return json.loads((TESTDATA / f"{name}.json").read_text(encoding="utf-8"))


def test_build_input_maps_every_role():
    steps = build_input(
        [
            Message("user", "哈囉"),
            Message("assistant", "我看一下", tool_calls=(ToolCall("fc_1", "expand_node", {"address": "TXabc"}),)),
            Message("tool", '{"edges": []}', tool_call_id="fc_1", name="expand_node"),
        ]
    )
    assert [step["type"] for step in steps] == [
        "user_input",
        "model_output",
        "function_result",
    ]
    assert steps[0]["content"] == [{"type": "text", "text": "哈囉"}]
    assert steps[2]["call_id"] == "fc_1"
    assert steps[2]["name"] == "expand_node"
    assert "is_error" not in steps[2]


def test_model_turn_is_replayed_verbatim():
    # Gemini 3.x 的 function_call 綁著一個 thought 簽章。自己重建湊不出簽章，
    # 送回去會被回 400 missing thought_signature，所以必須原樣回放。
    original = (
        {"type": "thought", "signature": "sig-abc"},
        {"type": "function_call", "id": "fc_1", "name": "expand_node",
         "arguments": {"address": "TXabc"}},
    )
    steps = build_input(
        [
            Message("user", "查一下"),
            Message("assistant", "", tool_calls=(ToolCall("fc_1", "expand_node", {}),),
                    provider_steps=original),
            Message("tool", "{}", tool_call_id="fc_1", name="expand_node"),
        ]
    )
    assert steps[1] is original[0], "thought 簽章必須原封不動"
    assert steps[2] is original[1], "function_call 必須原封不動"
    assert steps[3]["type"] == "function_result"


def test_history_without_signatures_falls_back_to_text():
    # 從資料庫讀回的歷史只剩文字，沒有簽章可回放。
    steps = build_input([Message("assistant", "先前的回答")])
    assert steps == [{"type": "model_output", "content": [{"type": "text", "text": "先前的回答"}]}]


def test_assistant_turn_without_steps_or_text_emits_nothing():
    steps = build_input(
        [
            Message("assistant", "", tool_calls=(ToolCall("fc_1", "expand_node", {"address": "TXabc"}),)),
            Message("tool", "{}", tool_call_id="fc_1", name="expand_node"),
        ]
    )
    assert [step["type"] for step in steps] == ["function_result"]


def test_build_input_marks_failed_tool_results():
    steps = build_input([Message("tool", "out_of_scope", tool_call_id="fc_9", is_error=True)])
    assert steps[0]["is_error"] is True


def test_build_input_omits_empty_assistant_text():
    steps = build_input(
        [Message("assistant", "", tool_calls=(ToolCall("fc_1", "get_transfers", {}),))]
    )
    assert steps == []


def test_build_request_is_always_stateless():
    request = build_request(model="m", system="S", messages=[Message("user", "hi")])
    # 對話歷史的 source of truth 在 Go，不准依賴 Google 端的狀態。
    assert request["store"] is False
    assert "previous_interaction_id" not in request
    assert request["system_instruction"] == "S"
    assert request["generation_config"]["max_output_tokens"] == 4096


def test_build_request_sets_thinking_level():
    # Gemini 3.x 一定會思考，壓低才不會每次都等很久。
    request = build_request(model="m", system="", messages=[Message("user", "hi")])
    assert request["generation_config"]["thinking_level"] == "low"

    slow = build_request(
        model="m", system="", messages=[Message("user", "hi")], thinking_level="high"
    )
    assert slow["generation_config"]["thinking_level"] == "high"


def test_build_request_omits_empty_system_and_tools():
    request = build_request(model="m", system="", messages=[Message("user", "hi")])
    assert "system_instruction" not in request
    assert "tools" not in request
    assert "response_format" not in request


def test_build_request_tool_and_schema_shape():
    request = build_request(
        model="m",
        system="S",
        messages=[Message("user", "hi")],
        tools=[ToolSpec("expand_node", "展開節點", {"type": "object", "properties": {}})],
        schema={"type": "object"},
        max_tokens=99,
    )
    assert request["tools"] == [
        {
            "type": "function",
            "name": "expand_node",
            "description": "展開節點",
            "parameters": {"type": "object", "properties": {}},
        }
    ]
    assert request["response_format"] == {
        "type": "text",
        "mime_type": "application/json",
        "schema": {"type": "object"},
    }
    assert request["generation_config"]["max_output_tokens"] == 99


def test_parse_reply_reads_tool_calls():
    reply = parse_reply(load("tool-call-turn"))
    assert reply.stop_reason == "tool_calls"
    assert len(reply.tool_calls) == 1
    call = reply.tool_calls[0]
    assert (call.id, call.name) == ("fc_1", "expand_node")
    assert call.arguments == {"address": "TXabc"}
    assert reply.usage.input_tokens == 1200
    assert reply.usage.output_tokens == 48


def test_parse_reply_reads_final_text():
    reply = parse_reply(load("final-turn"))
    assert reply.stop_reason == "stop"
    assert reply.tool_calls == ()
    assert "fan_out" in reply.text
    assert reply.usage.output_tokens == 95


def test_truncated_reply_is_reported_as_length():
    # status "incomplete" 代表撞到 max_output_tokens，output_text 是半截的。
    # 報成 "stop" 的話上層會把殘缺摘要當完整的存進資料庫。
    reply = parse_reply(load("truncated-turn"))
    assert reply.stop_reason == "length"
    assert reply.text.endswith("TSmokeTestAddress0")


def test_thought_tokens_are_visible():
    # 思考的 token 會佔用 max_tokens 額度，看不到就調不動預算。
    reply = parse_reply(load("truncated-turn"))
    assert reply.usage.thought_tokens == 1008
    assert reply.usage.output_tokens == 188


def test_parse_reply_raises_on_failed_status():
    with pytest.raises(LLMError):
        parse_reply({"status": "failed", "errors": [{"message": "boom"}]})


def test_parse_reply_tolerates_stringified_arguments():
    reply = parse_reply(
        {
            "status": "requires_action",
            "steps": [
                {"type": "function_call", "id": "c1", "name": "t", "arguments": '{"a": 1}'}
            ],
        }
    )
    assert reply.tool_calls[0].arguments == {"a": 1}


def test_parse_reply_survives_missing_optional_fields():
    reply = parse_reply({"status": "completed"})
    assert reply.text == ""
    assert reply.usage.input_tokens == 0
