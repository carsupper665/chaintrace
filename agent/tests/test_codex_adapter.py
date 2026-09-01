from collections import deque

import pytest

from llm import from_env
from llm.codex import CodexLLM, app_server_command
from llm.types import LLMError, Message, ToolSpec


class ScriptedServer:
    def __init__(self, messages):
        self.cwd = r"C:\empty-chaintrace-codex"
        self.messages = deque(messages)
        self.sent = []
        self.closed = False

    def send(self, message):
        self.sent.append(message)

    def receive(self, timeout):
        assert timeout > 0
        if not self.messages:
            raise AssertionError("test app-server has no scripted message")
        return self.messages.popleft()

    def close(self):
        self.closed = True


def opening_messages(*tail):
    return [
        {"id": 0, "result": {}},
        {"id": 1, "result": {"thread": {"id": "thr_1"}}},
        {"id": 2, "result": {"turn": {"id": "turn_1"}}},
        *tail,
    ]


def test_command_disables_codex_execution_capabilities():
    command = app_server_command("codex-test")
    assert command[:2] == ["codex-test", "app-server"]
    assert 'web_search="disabled"' in command
    assert "mcp_servers={}" in command
    for feature in ("shell_tool", "apps", "plugins", "multi_agent", "browser_use"):
        index = command.index(feature)
        assert command[index - 1] == "--disable"


def test_dynamic_tool_is_returned_to_go_and_result_resumes_same_turn():
    server = ScriptedServer(
        opening_messages(
            {
                "method": "item/started",
                "params": {"item": {"type": "dynamicToolCall"}},
            },
            {
                "method": "item/tool/call",
                "id": "server_req_1",
                "params": {
                    "tool": "expand_node",
                    "callId": "call_1",
                    "arguments": {"address": "TXabc"},
                },
            },
            {
                "method": "item/completed",
                "params": {"item": {"type": "dynamicToolCall"}},
            },
            {
                "method": "item/completed",
                "params": {
                    "item": {
                        "type": "agentMessage",
                        "text": "Go 回傳這個地址有兩筆關係。",
                    }
                },
            },
            {
                "method": "thread/tokenUsage/updated",
                "params": {
                    "tokenUsage": {
                        "last": {
                            "inputTokens": 120,
                            "outputTokens": 30,
                            "reasoningOutputTokens": 10,
                        }
                    }
                },
            },
            {
                "method": "turn/completed",
                "params": {"turn": {"status": "completed", "items": []}},
            },
        )
    )
    commands = []
    llm = CodexLLM(
        command="codex-test",
        model="gpt-test",
        reasoning_effort="low",
        server_factory=lambda command: commands.append(command) or server,
    )
    tool = ToolSpec(
        "expand_node",
        "Ask Go to disclose an existing node",
        {"type": "object", "properties": {"address": {"type": "string"}}},
    )

    first = llm.complete(
        system="Only use this evidence.",
        messages=[Message("user", "查這個地址")],
        tools=[tool],
    )

    assert commands == ["codex-test"]
    assert first.stop_reason == "tool_calls"
    assert first.tool_calls[0].name == "expand_node"
    assert first.tool_calls[0].arguments == {"address": "TXabc"}

    thread_start = server.sent[2]
    assert thread_start["method"] == "thread/start"
    assert thread_start["params"]["sandbox"] == "read-only"
    assert thread_start["params"]["approvalPolicy"] == "never"
    assert thread_start["params"]["ephemeral"] is True
    assert thread_start["params"]["model"] == "gpt-test"
    assert thread_start["params"]["dynamicTools"] == [
        {
            "type": "function",
            "name": "expand_node",
            "description": "Ask Go to disclose an existing node",
            "inputSchema": tool.parameters,
        }
    ]
    assert "only tools" in thread_start["params"]["baseInstructions"]
    assert thread_start["params"]["developerInstructions"] == "Only use this evidence."

    turn_start = server.sent[3]
    assert turn_start["params"]["sandboxPolicy"]["type"] == "readOnly"
    assert turn_start["params"]["sandboxPolicy"] == {"type": "readOnly"}
    assert turn_start["params"]["effort"] == "low"

    final = llm.complete(
        system="ignored while resuming",
        messages=[
            Message("user", "查這個地址"),
            Message(
                "assistant",
                tool_calls=first.tool_calls,
                provider_steps=first.provider_steps,
            ),
            Message(
                "tool",
                '{"edges": 2}',
                tool_call_id="call_1",
                name="expand_node",
            ),
        ],
        tools=[tool],
    )

    assert final.text == "Go 回傳這個地址有兩筆關係。"
    assert final.usage.input_tokens == 120
    assert final.usage.output_tokens == 30
    assert final.usage.thought_tokens == 10
    assert server.sent[-1] == {
        "id": "server_req_1",
        "result": {
            "contentItems": [{"type": "inputText", "text": '{"edges": 2}'}],
            "success": True,
        },
    }
    assert server.closed is True


def test_failed_go_tool_result_is_reported_to_codex():
    server = ScriptedServer(
        opening_messages(
            {
                "method": "item/tool/call",
                "id": 9,
                "params": {
                    "tool": "expand_node",
                    "callId": "call_9",
                    "arguments": {"address": "TXabc"},
                },
            },
            {
                "method": "item/completed",
                "params": {"item": {"type": "agentMessage", "text": "地址超出範圍。"}},
            },
            {
                "method": "turn/completed",
                "params": {"turn": {"status": "completed", "items": []}},
            },
        )
    )
    llm = CodexLLM(server_factory=lambda _: server)
    tool = ToolSpec("expand_node", "", {"type": "object"})
    first = llm.complete(system="S", messages=[Message("user", "查")], tools=[tool])
    llm.complete(
        system="S",
        messages=[
            Message("assistant", tool_calls=first.tool_calls, provider_steps=first.provider_steps),
            Message(
                "tool",
                "out_of_scope",
                tool_call_id="call_9",
                name="expand_node",
                is_error=True,
            ),
        ],
        tools=[tool],
    )
    assert server.sent[-1]["result"]["success"] is False


def test_unlisted_dynamic_tool_fails_closed():
    server = ScriptedServer(
        opening_messages(
            {
                "method": "item/tool/call",
                "id": 10,
                "params": {"tool": "shell", "callId": "bad", "arguments": {}},
            }
        )
    )
    llm = CodexLLM(server_factory=lambda _: server)
    with pytest.raises(LLMError, match="未經 Go 提供"):
        llm.complete(system="S", messages=[Message("user", "查")])
    assert server.closed is True


def test_builtin_tool_item_fails_closed():
    server = ScriptedServer(
        opening_messages(
            {
                "method": "item/started",
                "params": {"item": {"type": "commandExecution", "command": "dir"}},
            }
        )
    )
    llm = CodexLLM(server_factory=lambda _: server)
    with pytest.raises(LLMError, match="禁止的能力"):
        llm.complete(system="S", messages=[Message("user", "查")])
    assert server.closed is True


def test_codex_provider_does_not_require_api_key(monkeypatch):
    monkeypatch.setenv("LLM_PROVIDER", "codex")
    monkeypatch.delenv("LLM_API_KEY", raising=False)
    monkeypatch.setenv("CODEX_COMMAND", "codex-custom")
    llm = from_env()
    assert isinstance(llm, CodexLLM)
    assert llm._command == "codex-custom"
    llm.close()


def test_structured_output_is_rejected_explicitly():
    llm = CodexLLM(server_factory=lambda _: None)
    with pytest.raises(LLMError, match="不支援結構化輸出"):
        llm.complete(system="S", messages=[Message("user", "查")], schema={"type": "object"})
