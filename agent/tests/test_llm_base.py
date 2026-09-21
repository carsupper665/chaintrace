"""llm.base 的共用骨架測試：走訪、回放、組 Reply、錯誤分類。

不碰任何 SDK。Gemini 專屬的欄位形狀在 test_gemini_adapter.py。
"""

import json
import os

import pytest

from llm.base import (
    LLM,
    BaseLLM,
    classify_error,
    complete_as_stream,
    dump_failed_request,
    parse_arguments,
    resolve_stop_reason,
)
from llm.fake import FakeLLM
from llm.types import LLMError, Message, Reply, StreamDone, TextDelta, ToolCall, ToolSpec, Usage


class DictLLM(BaseLLM):
    """最小的 adapter：wire format 是 dict，payload 也是 dict。用來單獨測骨架。"""

    provider = "Dict"

    def __init__(self, *, sender=None):
        super().__init__(model="m", timeout=7.0)
        self._sender = sender
        self.clients_made = 0

    def _make_client(self):
        self.clients_made += 1
        return object()

    def format_user(self, message):
        return {"role": "user", "text": message.content}

    def format_assistant(self, message):
        return [{"role": "assistant", "text": message.content}] if message.content else []

    def format_tool_result(self, message):
        return {"role": "tool", "id": message.tool_call_id}

    def build_request(self, *, system, messages, tools=(), schema=None, max_tokens=4096):
        return {"system": system, "input": self.build_input(messages), "tools": self.build_tools(tools)}

    def _send(self, request):
        return self._sender(request)

    def failure_of(self, payload):
        return payload.get("error")

    def text_of(self, payload):
        return payload.get("text", "")

    def raw_tool_calls(self, payload):
        return payload.get("calls", [])

    def usage_of(self, payload):
        return Usage(*payload.get("usage", (0, 0, 0)))

    def is_truncated(self, payload):
        return payload.get("truncated", False)

    def is_refused(self, payload):
        return payload.get("refused", False)


# ---- prompt 組裝 ----


def test_build_input_dispatches_every_role_in_order():
    steps = FakeLLM().build_input(
        [
            Message("user", "哈囉"),
            Message("assistant", "我看一下"),
            Message("tool", "{}", tool_call_id="fc_1"),
        ]
    )
    assert [step.role for step in steps] == ["user", "assistant", "tool"]


def test_assistant_turn_with_provider_steps_is_replayed_verbatim():
    original = ({"type": "thought", "signature": "sig"}, {"type": "function_call", "id": "fc_1"})
    steps = DictLLM().build_input(
        [Message("assistant", "會被忽略的文字", provider_steps=original)]
    )
    assert steps[0] is original[0]
    assert steps[1] is original[1]


def test_assistant_turn_without_provider_steps_uses_the_formatter():
    steps = DictLLM().build_input([Message("assistant", "從資料庫讀回的")])
    assert steps == [{"role": "assistant", "text": "從資料庫讀回的"}]
    assert DictLLM().build_input([Message("assistant", "")]) == []


def test_build_input_rejects_unknown_roles():
    with pytest.raises(LLMError, match="未知的訊息 role"):
        DictLLM().build_input([Message("system", "x")])  # type: ignore[arg-type]


def test_default_tool_declaration_is_chat_completions_shaped():
    tools = DictLLM().build_tools(
        [ToolSpec("expand_node", "展開節點", {"type": "object", "properties": {}})]
    )
    assert tools == [
        {
            "type": "function",
            "function": {
                "name": "expand_node",
                "description": "展開節點",
                "parameters": {"type": "object", "properties": {}},
            },
        }
    ]


class ChatLLM(DictLLM):
    """只覆寫回覆那一側，request 那一側全用 base 預設（Chat Completions 形狀）。"""

    format_user = BaseLLM.format_user
    format_assistant = BaseLLM.format_assistant
    format_tool_result = BaseLLM.format_tool_result


def test_default_format_hooks_speak_chat_completions():
    steps = ChatLLM().build_input(
        [
            Message("user", "哈囉"),
            Message("assistant", "我看一下", tool_calls=(ToolCall("fc_1", "expand_node", {"address": "TXabc"}),)),
            Message("tool", '{"edges": []}', tool_call_id="fc_1", name="expand_node"),
        ]
    )
    assert steps[0] == {"role": "user", "content": "哈囉"}
    assert steps[1]["role"] == "assistant"
    assert steps[1]["content"] == "我看一下"
    # Chat Completions 要求 arguments 是 JSON 字串，且 tool 結果前要有這則 tool_calls。
    assert steps[1]["tool_calls"] == [
        {"id": "fc_1", "type": "function",
         "function": {"name": "expand_node", "arguments": '{"address": "TXabc"}'}}
    ]
    assert steps[2] == {"role": "tool", "tool_call_id": "fc_1", "content": '{"edges": []}'}


def test_default_assistant_format_handles_text_only_and_empty():
    assert ChatLLM().build_input([Message("assistant", "純文字")]) == [
        {"role": "assistant", "content": "純文字"}
    ]
    assert ChatLLM().build_input([Message("assistant", "")]) == []
    only_calls = ChatLLM().build_input(
        [Message("assistant", "", tool_calls=(ToolCall("c", "t", {}),))]
    )
    assert only_calls[0]["content"] is None
    assert only_calls[0]["tool_calls"][0]["id"] == "c"


def test_base_constructor_keeps_the_common_fields():
    llm = DictLLM()
    assert (llm._model, llm.name, llm._timeout) == ("m", "m", 7.0)
    assert llm._api_key == "" and llm._base_url == "" and llm._level == ""
    assert llm._extra_params == {}
    assert llm._client is None


def test_connect_builds_the_client_once():
    llm = DictLLM()
    first = llm._connect()
    assert llm._connect() is first
    assert llm.clients_made == 1


# ---- 回覆處理 ----


def test_parse_reply_assembles_a_neutral_reply():
    reply = DictLLM().parse_reply(
        {
            "text": "看一下",
            "calls": [("c1", "expand_node", '{"address": "TXabc"}')],
            "usage": (10, 5, 2),
        }
    )
    assert reply.text == "看一下"
    assert reply.tool_calls == (ToolCall("c1", "expand_node", {"address": "TXabc"}),)
    assert reply.stop_reason == "tool_calls"
    assert reply.usage == Usage(10, 5, 2)
    assert reply.provider_steps == ()


def test_parse_reply_reports_failure_with_the_provider_name():
    with pytest.raises(LLMError, match="Dict 回覆失敗: boom"):
        DictLLM().parse_reply({"error": "boom"})


def test_parse_reply_maps_truncation_and_refusal():
    assert DictLLM().parse_reply({"truncated": True}).stop_reason == "length"
    assert DictLLM().parse_reply({"refused": True}).stop_reason == "refusal"
    # 有 tool call 就是 tool_calls，不管其他旗標。
    truncated_call = {"truncated": True, "calls": [("c1", "t", {})]}
    assert DictLLM().parse_reply(truncated_call).stop_reason == "tool_calls"


@pytest.mark.parametrize(
    "reply",
    [
        Reply(text="沒有異常", usage=Usage(10, 5)),
        Reply(tool_calls=(ToolCall("fc_1", "expand_node", {"address": "TXabc"}),), stop_reason="tool_calls",
              provider_steps=({"type": "thought"},)),
        Reply(text="半截", stop_reason="length", usage=Usage(1, 2, 3)),
        Reply(stop_reason="refusal"),
    ],
)
def test_fake_llm_round_trips_its_scripted_reply(reply):
    assert FakeLLM().parse_reply(reply) == reply


def test_parse_arguments_accepts_dict_string_and_missing():
    assert parse_arguments({"a": 1}) == {"a": 1}
    assert parse_arguments('{"a": 1}') == {"a": 1}
    assert parse_arguments(None) == {}
    with pytest.raises(LLMError, match="不是合法 JSON"):
        parse_arguments("{nope")


def test_resolve_stop_reason_precedence():
    call = (ToolCall("c", "t", {}),)
    assert resolve_stop_reason(tool_calls=call, truncated=True, refused=True) == "tool_calls"
    assert resolve_stop_reason(tool_calls=(), truncated=True, refused=True) == "length"
    assert resolve_stop_reason(tool_calls=(), refused=True) == "refusal"
    assert resolve_stop_reason(tool_calls=()) == "stop"


# ---- 錯誤分類 ----


class StatusError(Exception):
    def __init__(self, status_code: int, text: str = "x"):
        super().__init__(text)
        self.status_code = status_code


class CodeError(Exception):
    def __init__(self, code: int, text: str = "x"):
        super().__init__(text)
        self.code = code


@pytest.mark.parametrize(
    ("error", "retryable", "fragment"),
    [
        (RuntimeError("Request timed out"), True, "在 7 秒內沒有回應"),
        (RuntimeError("Error code: 400 - bad"), False, "拒絕這個請求"),
        (StatusError(400), False, "拒絕這個請求"),
        (CodeError(401), False, "請檢查 LLM_API_KEY"),
        (StatusError(403), False, "請檢查 LLM_API_KEY"),
        (RuntimeError("error code: 429"), True, "額度或速率上限"),
        (StatusError(429), True, "額度或速率上限"),
        (RuntimeError("model is under high demand"), True, "目前過載"),
        (CodeError(503), True, "目前過載"),
        (RuntimeError("something else"), True, "呼叫失敗"),
    ],
)
def test_classify_error_rules(error, retryable, fragment):
    classified = classify_error(error, provider="P", timeout=7.0)
    assert isinstance(classified, LLMError)
    assert classified.retryable is retryable
    assert str(classified).startswith("P ")
    assert fragment in str(classified)


def test_classify_error_appends_the_adapter_hint():
    classified = classify_error(
        StatusError(429, "slow down"),
        provider="P",
        timeout=1.0,
        hints={"rate_limited": "（換模型可繼續）"},
    )
    assert str(classified) == "P 額度或速率上限（換模型可繼續）：slow down"


def test_classify_error_ignores_bool_codes():
    # bool 是 int 的子型別；code=True 不該被當成狀態碼 1。
    error = CodeError(True, "weird")  # type: ignore[arg-type]
    assert "呼叫失敗" in str(classify_error(error, provider="P", timeout=1.0))


# ---- debug dump ----


def test_dump_failed_request_writes_when_enabled(tmp_path, monkeypatch):
    target = tmp_path / "dump.json"
    monkeypatch.setenv("AGENT_DEBUG_DUMP", str(target))
    dump_failed_request({"model": "m", "input": [{"role": "user"}]})
    assert json.loads(target.read_text(encoding="utf-8"))["model"] == "m"


def test_dump_failed_request_is_a_noop_when_disabled(tmp_path, monkeypatch):
    monkeypatch.delenv("AGENT_DEBUG_DUMP", raising=False)
    dump_failed_request({"model": "m"})
    assert os.listdir(tmp_path) == []


def test_dump_failed_request_swallows_os_errors(tmp_path, monkeypatch):
    monkeypatch.setenv("AGENT_DEBUG_DUMP", str(tmp_path / "missing-dir" / "dump.json"))
    dump_failed_request({"model": "m"})  # 不能丟例外，否則真正的錯誤會被蓋掉


# ---- complete() 骨架 ----


def test_complete_wraps_sdk_errors_and_dumps_the_request(tmp_path, monkeypatch):
    target = tmp_path / "dump.json"
    monkeypatch.setenv("AGENT_DEBUG_DUMP", str(target))
    cause = StatusError(400, "bad field")
    llm = DictLLM(sender=lambda request: (_ for _ in ()).throw(cause))

    with pytest.raises(LLMError, match="Dict 拒絕這個請求") as info:
        llm.complete(system="S", messages=[Message("user", "hi")])

    assert info.value.__cause__ is cause
    assert info.value.retryable is False
    assert json.loads(target.read_text(encoding="utf-8"))["system"] == "S"


def test_complete_passes_adapter_llm_errors_through_untouched():
    own = LLMError("adapter 自己判定的", retryable=True)

    def sender(request):
        raise own

    with pytest.raises(LLMError) as info:
        DictLLM(sender=sender).complete(system="S", messages=[Message("user", "hi")])
    assert info.value is own


def test_complete_returns_the_parsed_reply():
    seen = []

    def sender(request):
        seen.append(request)
        return {"text": "ok", "usage": (3, 1, 0)}

    reply = DictLLM(sender=sender).complete(
        system="S",
        messages=[Message("user", "hi")],
        tools=[ToolSpec("t", "d", {"type": "object"})],
    )
    assert reply == Reply(text="ok", usage=Usage(3, 1, 0))
    assert seen[0]["input"] == [{"role": "user", "text": "hi"}]
    assert seen[0]["tools"][0]["function"]["name"] == "t"


def test_fake_llm_is_a_base_llm_and_an_llm():
    llm = FakeLLM()
    assert isinstance(llm, BaseLLM)
    assert isinstance(llm, LLM)


# ---- stream() 骨架：沒有真的串流能力時墊一個一次性 chunk ----


def test_complete_as_stream_yields_one_text_delta_then_done():
    reply = Reply(text="看一下", usage=Usage(3, 1))
    chunks = list(complete_as_stream(lambda **_: reply, system="S", messages=[]))
    assert chunks == [TextDelta("看一下"), StreamDone(reply)]


def test_complete_as_stream_skips_the_text_delta_when_reply_has_no_text():
    reply = Reply(tool_calls=(ToolCall("c", "t", {}),), stop_reason="tool_calls")
    chunks = list(complete_as_stream(lambda **_: reply, system="S", messages=[]))
    assert chunks == [StreamDone(reply)]


def test_base_llm_stream_default_delegates_to_complete():
    reply = Reply(text="ok", usage=Usage(3, 1, 0))
    llm = DictLLM(sender=lambda request: {"text": "ok", "usage": (3, 1, 0)})
    chunks = list(llm.stream(system="S", messages=[Message("user", "hi")]))
    assert chunks == [TextDelta("ok"), StreamDone(reply)]
