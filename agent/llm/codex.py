"""Codex app-server adapter.

Codex runs in an empty, read-only sandbox. The only accepted tool requests are
the dynamic tools supplied by Go; their execution still happens in Go's
existing audited tool loop.
"""

from __future__ import annotations

import atexit
import json
import os
import queue
import subprocess
import tempfile
import threading
import time
import uuid
from collections import deque
from dataclasses import dataclass, field
from typing import Callable, Sequence

from .types import LLMError, Message, Reply, ToolCall, ToolSpec, Usage

DEFAULT_COMMAND = "codex"
DEFAULT_TIMEOUT_SECONDS = 120.0

_BASE_INSTRUCTIONS = """You are the read-only ChainTrace investigation model.
Answer only from the developer instructions and conversation supplied by the client.
The only tools you may use are the client-supplied dynamic tools, which request work
from the Go backend. Never use shell, filesystem, file changes, web, browser, apps,
MCP, plugins, skills, computer use, subagents, or user-input tools. Never modify local
or external state. If the evidence is insufficient, say so."""

_DISABLED_FEATURES = (
    "shell_tool",
    "unified_exec",
    "apps",
    "plugins",
    "multi_agent",
    "multi_agent_v2",
    "browser_use",
    "in_app_browser",
    "computer_use",
    "image_generation",
    "view_image",
    "workspace_dependencies",
    "skill_search",
    "tool_suggest",
    "hooks",
    "goals",
    "code_mode",
)

_SAFE_ITEM_TYPES = {
    "userMessage",
    "agentMessage",
    "reasoning",
    "plan",
    "dynamicToolCall",
    "contextCompaction",
}


def app_server_command(command: str = DEFAULT_COMMAND) -> list[str]:
    """Build the locked-down app-server command without invoking a shell."""
    args = [command, "app-server", "-c", 'web_search="disabled"', "-c", "mcp_servers={}"]
    for feature in _DISABLED_FEATURES:
        args.extend(("--disable", feature))
    return args


def _conversation_text(messages: Sequence[Message]) -> str:
    parts: list[str] = []
    for message in messages:
        if message.role == "user":
            parts.append(f"Owner:\n{message.content}")
        elif message.role == "assistant":
            if message.content:
                parts.append(f"Agent:\n{message.content}")
            for call in message.tool_calls:
                arguments = json.dumps(call.arguments, ensure_ascii=False, sort_keys=True)
                parts.append(f"Agent requested Go tool {call.name} ({call.id}):\n{arguments}")
        elif message.role == "tool":
            status = "failed" if message.is_error else "succeeded"
            parts.append(
                f"Go tool {message.name or 'unknown'} ({message.tool_call_id}) {status}:\n"
                f"{message.content}"
            )
        else:
            raise LLMError(f"未知的訊息 role: {message.role}")
    if not parts:
        raise LLMError("Codex turn 沒有訊息")
    return "\n\n".join(parts)


class _AppServerProcess:
    """One stdio app-server process for one Agent Turn."""

    def __init__(self, command: str):
        self._directory = tempfile.TemporaryDirectory(prefix="chaintrace-codex-")
        self.cwd = self._directory.name
        self._stdout: queue.Queue[str | None] = queue.Queue()
        self._stderr: deque[str] = deque(maxlen=20)
        creationflags = getattr(subprocess, "CREATE_NO_WINDOW", 0)
        try:
            self._process = subprocess.Popen(
                app_server_command(command),
                cwd=self.cwd,
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                encoding="utf-8",
                bufsize=1,
                creationflags=creationflags,
            )
        except OSError as error:
            self._directory.cleanup()
            raise LLMError(f"無法啟動 Codex app-server: {error}") from error
        threading.Thread(target=self._read_stdout, daemon=True).start()
        threading.Thread(target=self._read_stderr, daemon=True).start()

    def _read_stdout(self) -> None:
        assert self._process.stdout is not None
        for line in self._process.stdout:
            self._stdout.put(line)
        self._stdout.put(None)

    def _read_stderr(self) -> None:
        assert self._process.stderr is not None
        for line in self._process.stderr:
            self._stderr.append(line.strip())

    def send(self, message: dict) -> None:
        if self._process.poll() is not None or self._process.stdin is None:
            raise self._stopped_error()
        try:
            self._process.stdin.write(json.dumps(message, ensure_ascii=False) + "\n")
            self._process.stdin.flush()
        except OSError as error:
            raise self._stopped_error() from error

    def receive(self, timeout: float) -> dict:
        try:
            line = self._stdout.get(timeout=max(timeout, 0.001))
        except queue.Empty as error:
            raise LLMError("Codex app-server 逾時", retryable=True) from error
        if line is None:
            raise self._stopped_error()
        try:
            message = json.loads(line)
        except ValueError as error:
            raise LLMError(f"Codex app-server 回傳無效 JSON: {error}") from error
        if not isinstance(message, dict):
            raise LLMError("Codex app-server 回傳格式錯誤")
        return message

    def _stopped_error(self) -> LLMError:
        detail = self._stderr[-1] if self._stderr else "no stderr"
        return LLMError(f"Codex app-server 已停止: {detail}", retryable=True)

    def close(self) -> None:
        if self._process.poll() is None:
            self._process.terminate()
            try:
                self._process.wait(timeout=1)
            except subprocess.TimeoutExpired:
                self._process.kill()
                self._process.wait(timeout=1)
        self._directory.cleanup()


@dataclass
class _PendingTurn:
    server: object
    allowed_tools: frozenset[str]
    token: str = field(default_factory=lambda: uuid.uuid4().hex)
    requests: dict[str, object] = field(default_factory=dict)
    text: str = ""
    usage: Usage = field(default_factory=Usage)
    lock: threading.Lock = field(default_factory=threading.Lock)
    expiry: threading.Timer | None = None


class CodexLLM:
    """LLM adapter backed by a locally authenticated Codex app-server."""

    def __init__(
        self,
        *,
        command: str = DEFAULT_COMMAND,
        model: str = "",
        timeout: float = DEFAULT_TIMEOUT_SECONDS,
        reasoning_effort: str = "",
        server_factory: Callable[[str], object] | None = None,
    ):
        self._command = command
        self._model = model
        self._timeout = timeout
        self._reasoning_effort = reasoning_effort
        self._server_factory = server_factory or _AppServerProcess
        self._pending: dict[str, _PendingTurn] = {}
        self._lock = threading.Lock()
        atexit.register(self.close)

    def complete(
        self,
        *,
        system: str,
        messages: Sequence[Message],
        tools: Sequence[ToolSpec] = (),
        schema: dict | None = None,
        max_tokens: int = 4096,
    ) -> Reply:
        del max_tokens  # app-server owns the model output budget
        if schema is not None:
            raise LLMError("Codex app-server adapter 不支援結構化輸出")

        token = self._state_token(messages)
        with self._lock:
            pending = self._pending.get(token) if token else None
        if pending is None:
            return self._start(system, messages, tools)
        return self._continue(pending, messages)

    def _start(
        self, system: str, messages: Sequence[Message], tools: Sequence[ToolSpec]
    ) -> Reply:
        server = self._server_factory(self._command)
        pending = _PendingTurn(server=server, allowed_tools=frozenset(t.name for t in tools))
        deadline = time.monotonic() + self._timeout
        try:
            self._rpc(
                pending,
                0,
                "initialize",
                {
                    "clientInfo": {
                        "name": "chaintrace_agent",
                        "title": "ChainTrace Agent",
                        "version": "0.1.0",
                    },
                    "capabilities": {"experimentalApi": True},
                },
                deadline,
            )
            server.send({"method": "initialized", "params": {}})
            thread_params: dict = {
                "cwd": server.cwd,
                "sandbox": "read-only",
                "approvalPolicy": "never",
                "ephemeral": True,
                "serviceName": "chaintrace_agent",
                "baseInstructions": _BASE_INSTRUCTIONS,
                "developerInstructions": system,
                "dynamicTools": [self._dynamic_tool(tool) for tool in tools],
            }
            if self._model:
                thread_params["model"] = self._model
            started = self._rpc(pending, 1, "thread/start", thread_params, deadline)
            thread_id = ((started.get("result") or {}).get("thread") or {}).get("id")
            if not thread_id:
                raise LLMError("Codex app-server 沒有回傳 thread id")
            turn_params: dict = {
                "threadId": thread_id,
                "input": [{"type": "text", "text": _conversation_text(messages)}],
                "cwd": server.cwd,
                "approvalPolicy": "never",
                "sandboxPolicy": {"type": "readOnly"},
            }
            if self._model:
                turn_params["model"] = self._model
            if self._reasoning_effort:
                turn_params["effort"] = self._reasoning_effort
            self._rpc(pending, 2, "turn/start", turn_params, deadline)
            return self._read_turn(pending, deadline)
        except Exception:
            self._discard(pending)
            raise

    def _continue(self, pending: _PendingTurn, messages: Sequence[Message]) -> Reply:
        with pending.lock:
            if pending.expiry is not None:
                pending.expiry.cancel()
                pending.expiry = None
            try:
                results = self._tool_results(messages, pending.token)
                if not results:
                    raise LLMError("Codex tool continuation 沒有 Go tool result")
                for result in results:
                    request_id = pending.requests.pop(result.tool_call_id, None)
                    if request_id is None:
                        raise LLMError(f"找不到 Codex tool call: {result.tool_call_id}")
                    pending.server.send(
                        {
                            "id": request_id,
                            "result": {
                                "contentItems": [
                                    {"type": "inputText", "text": result.content}
                                ],
                                "success": not result.is_error,
                            },
                        }
                    )
                return self._read_turn(pending, time.monotonic() + self._timeout)
            except Exception:
                self._discard(pending)
                raise

    def _read_turn(self, pending: _PendingTurn, deadline: float) -> Reply:
        while True:
            message = pending.server.receive(self._remaining(deadline))
            method = message.get("method")
            params = message.get("params") or {}

            if method == "item/tool/call":
                tool = params.get("tool") or ""
                call_id = params.get("callId") or ""
                arguments = params.get("arguments")
                if tool not in pending.allowed_tools or not call_id or not isinstance(arguments, dict):
                    raise LLMError("Codex 嘗試呼叫未經 Go 提供的工具")
                pending.requests[call_id] = message.get("id")
                self._keep(pending)
                return Reply(
                    tool_calls=(ToolCall(call_id, tool, arguments),),
                    stop_reason="tool_calls",
                    usage=pending.usage,
                    provider_steps=({"type": "codex_app_server_state", "token": pending.token},),
                )

            if method in {"item/started", "item/completed"}:
                item = params.get("item") or {}
                item_type = item.get("type")
                if item_type not in _SAFE_ITEM_TYPES:
                    raise LLMError(f"Codex 嘗試使用禁止的能力: {item_type or 'unknown'}")
                if method == "item/completed" and item_type == "agentMessage":
                    text = item.get("text") or ""
                    if text:
                        pending.text = text
                continue

            if method == "thread/tokenUsage/updated":
                usage = ((params.get("tokenUsage") or {}).get("last") or {})
                pending.usage = Usage(
                    input_tokens=usage.get("inputTokens") or 0,
                    output_tokens=usage.get("outputTokens") or 0,
                    thought_tokens=usage.get("reasoningOutputTokens") or 0,
                )
                continue

            if method == "turn/completed":
                turn = params.get("turn") or {}
                if turn.get("status") != "completed":
                    error = (turn.get("error") or {}).get("message") or turn.get("status")
                    raise LLMError(f"Codex turn 失敗: {error}", retryable=True)
                if not pending.text:
                    for item in reversed(turn.get("items") or []):
                        if item.get("type") == "agentMessage" and item.get("text"):
                            pending.text = item["text"]
                            break
                if not pending.text:
                    raise LLMError("Codex 沒有產生答案")
                reply = Reply(text=pending.text, usage=pending.usage)
                self._discard(pending)
                return reply

            if method and "id" in message:
                raise LLMError(f"Codex 嘗試使用禁止的 server request: {method}")
            if message.get("error"):
                raise LLMError(self._rpc_error(message), retryable=True)

    def _rpc(
        self, pending: _PendingTurn, request_id: int, method: str, params: dict, deadline: float
    ) -> dict:
        pending.server.send({"method": method, "id": request_id, "params": params})
        while True:
            message = pending.server.receive(self._remaining(deadline))
            if message.get("id") != request_id:
                continue
            if message.get("error"):
                raise LLMError(self._rpc_error(message))
            return message

    @staticmethod
    def _dynamic_tool(tool: ToolSpec) -> dict:
        return {
            "type": "function",
            "name": tool.name,
            "description": tool.description,
            "inputSchema": tool.parameters,
        }

    @staticmethod
    def _state_token(messages: Sequence[Message]) -> str:
        for message in reversed(messages):
            if message.role != "assistant":
                continue
            for step in message.provider_steps:
                if isinstance(step, dict) and step.get("type") == "codex_app_server_state":
                    return step.get("token") or ""
        return ""

    @staticmethod
    def _tool_results(messages: Sequence[Message], token: str) -> list[Message]:
        state_index = -1
        for index, message in enumerate(messages):
            if any(
                isinstance(step, dict)
                and step.get("type") == "codex_app_server_state"
                and step.get("token") == token
                for step in message.provider_steps
            ):
                state_index = index
        return [message for message in messages[state_index + 1 :] if message.role == "tool"]

    @staticmethod
    def _remaining(deadline: float) -> float:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise LLMError("Codex app-server 逾時", retryable=True)
        return remaining

    @staticmethod
    def _rpc_error(message: dict) -> str:
        error = message.get("error") or {}
        return f"Codex app-server error: {error.get('message') or error}"

    def _keep(self, pending: _PendingTurn) -> None:
        with self._lock:
            self._pending[pending.token] = pending
        expiry = threading.Timer(max(self._timeout * 2, 60), self._expire, (pending.token,))
        expiry.daemon = True
        pending.expiry = expiry
        expiry.start()

    def _expire(self, token: str) -> None:
        with self._lock:
            pending = self._pending.pop(token, None)
        if pending is not None:
            pending.server.close()

    def _discard(self, pending: _PendingTurn) -> None:
        with self._lock:
            self._pending.pop(pending.token, None)
        if pending.expiry is not None:
            pending.expiry.cancel()
            pending.expiry = None
        pending.server.close()

    def close(self) -> None:
        with self._lock:
            pending = list(self._pending.values())
            self._pending.clear()
        for turn in pending:
            if turn.expiry is not None:
                turn.expiry.cancel()
            turn.server.close()
