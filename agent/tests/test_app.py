"""HTTP 層測試。用 FakeLLM，不碰網路、不需要金鑰。"""

from fastapi.testclient import TestClient

from app import create_app
from llm.fake import FakeLLM
from llm.types import Reply, ToolCall, Usage
from session import SessionStore

KEY = "test-key"
HEADERS = {"X-Agent-Key": KEY}

EVIDENCE = {"dataset": {"partial": False, "stopReason": "source_exhausted"}}
TOOLS = [{"name": "expand_node", "description": "展開節點", "parameters": {"type": "object"}}]


def build(replies, monkeypatch):
    monkeypatch.setenv("AGENT_SHARED_KEY", KEY)
    llm = FakeLLM(replies)
    store = SessionStore()
    return TestClient(create_app(llm=llm, store=store)), llm, store


def first_turn(**overrides) -> dict:
    payload = {
        "session_id": "inv1",
        "dataset_id": "ds1",
        "mode": "chat",
        "evidence": EVIDENCE,
        "messages": [{"role": "user", "content": "TXabc 有什麼異常?"}],
        "tools": TOOLS,
    }
    payload.update(overrides)
    return payload


def continuation(**overrides) -> dict:
    payload = {
        "session_id": "inv1",
        "dataset_id": "ds1",
        "tool_results": [
            {"call_id": "fc_1", "name": "expand_node", "content": '{"edges": 12}'}
        ],
    }
    payload.update(overrides)
    return payload


def calling_reply() -> Reply:
    return Reply(
        tool_calls=(ToolCall("fc_1", "expand_node", {"address": "TXabc"}),),
        stop_reason="tool_calls",
    )


def test_healthz_needs_no_key(monkeypatch):
    client, _, _ = build([], monkeypatch)
    response = client.get("/healthz")
    assert response.status_code == 200
    assert response.json()["status"] == "ok"


def test_missing_key_is_rejected(monkeypatch):
    client, _, _ = build([Reply(text="hi")], monkeypatch)
    assert client.post("/v1/agent/chat", json=first_turn()).status_code == 401


def test_wrong_key_is_rejected(monkeypatch):
    client, _, _ = build([Reply(text="hi")], monkeypatch)
    response = client.post("/v1/agent/chat", json=first_turn(), headers={"X-Agent-Key": "nope"})
    assert response.status_code == 401
    assert response.json()["code"] == "unauthorized"


def test_plain_turn_returns_text(monkeypatch):
    client, llm, _ = build([Reply(text="沒有異常", usage=Usage(10, 5))], monkeypatch)
    response = client.post("/v1/agent/chat", json=first_turn(), headers=HEADERS)
    assert response.status_code == 200
    body = response.json()
    assert body["text"] == "沒有異常"
    assert body["stop_reason"] == "stop"
    assert body["usage"] == {"input_tokens": 10, "output_tokens": 5}
    # 證據與工具有真的傳到 LLM。
    assert "source_exhausted" in llm.calls[0]["system"]
    assert llm.calls[0]["tools"][0].name == "expand_node"


def test_tool_call_is_returned_to_go(monkeypatch):
    client, _, _ = build([calling_reply()], monkeypatch)
    body = client.post("/v1/agent/chat", json=first_turn(), headers=HEADERS).json()
    assert body["stop_reason"] == "tool_calls"
    assert body["tool_calls"] == [
        {"id": "fc_1", "name": "expand_node", "arguments": {"address": "TXabc"}}
    ]


def test_continuation_resumes_the_same_session(monkeypatch):
    final = Reply(text="TXabc 轉出給 12 個地址")
    client, llm, _ = build([calling_reply(), final], monkeypatch)

    client.post("/v1/agent/chat", json=first_turn(), headers=HEADERS)
    response = client.post("/v1/agent/chat", json=continuation(), headers=HEADERS)

    assert response.status_code == 200
    assert response.json()["text"] == "TXabc 轉出給 12 個地址"
    # 第二次呼叫看得到 user 訊息、模型那一手、以及工具結果。
    assert [m.role for m in llm.calls[1]["messages"]] == ["user", "assistant", "tool"]
    # 續跑不必重送 evidence，system prompt 是 session 裡快取的那份。
    assert llm.calls[1]["system"] == llm.calls[0]["system"]


def test_finished_turn_clears_pending(monkeypatch):
    client, _, store = build([Reply(text="done")], monkeypatch)
    client.post("/v1/agent/chat", json=first_turn(), headers=HEADERS)
    session = store.get("inv1", "ds1")
    assert session is not None
    assert session.pending == []


def test_continuation_without_session_asks_go_to_resend(monkeypatch):
    client, _, _ = build([], monkeypatch)
    response = client.post(
        "/v1/agent/chat", json=continuation(session_id="gone"), headers=HEADERS
    )
    assert response.status_code == 409
    assert response.json()["code"] == "session_expired"


def test_new_dataset_invalidates_the_session(monkeypatch):
    client, _, _ = build([calling_reply()], monkeypatch)
    client.post("/v1/agent/chat", json=first_turn(), headers=HEADERS)
    response = client.post("/v1/agent/chat", json=continuation(dataset_id="ds2"), headers=HEADERS)
    assert response.status_code == 409


def test_first_turn_without_evidence_is_rejected(monkeypatch):
    client, _, _ = build([], monkeypatch)
    response = client.post(
        "/v1/agent/chat",
        json={"session_id": "inv1", "dataset_id": "ds1", "messages": []},
        headers=HEADERS,
    )
    assert response.status_code == 400
    assert response.json()["code"] == "invalid_turn"


def test_llm_failure_becomes_502(monkeypatch):
    client, _, _ = build([], monkeypatch)  # FakeLLM 沒有預設回覆 -> LLMError
    response = client.post("/v1/agent/chat", json=first_turn(), headers=HEADERS)
    assert response.status_code == 502
    assert response.json()["code"] == "llm_error"


def test_summary_mode_uses_the_summary_prompt(monkeypatch):
    client, llm, _ = build([Reply(text="摘要")], monkeypatch)
    client.post("/v1/agent/chat", json=first_turn(mode="summary"), headers=HEADERS)
    assert "分析範圍：" in llm.calls[0]["system"]


def test_failed_tool_result_is_passed_through(monkeypatch):
    client, llm, _ = build([calling_reply(), Reply(text="該地址不在範圍內")], monkeypatch)
    client.post("/v1/agent/chat", json=first_turn(), headers=HEADERS)
    client.post(
        "/v1/agent/chat",
        json=continuation(
            tool_results=[
                {"call_id": "fc_1", "name": "expand_node", "content": "out_of_scope", "is_error": True}
            ]
        ),
        headers=HEADERS,
    )
    tool_message = llm.calls[1]["messages"][-1]
    assert tool_message.is_error is True
    assert tool_message.content == "out_of_scope"


def test_unknown_fields_are_ignored(monkeypatch):
    client, _, _ = build([Reply(text="ok")], monkeypatch)
    payload = first_turn()
    payload["someFutureField"] = {"x": 1}
    assert client.post("/v1/agent/chat", json=payload, headers=HEADERS).status_code == 200
