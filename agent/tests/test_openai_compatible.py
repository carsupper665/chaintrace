"""OpenAI 相容 adapter 的翻譯測試與 model_factory 測試。

不碰網路、不需要金鑰、不需要裝 openai（client 是 lazy 的）。
"""

import json
import pathlib
import sys

import pytest

from llm import openai_compatible
from llm.openai_compatible import (
    CONFIG_DIR,
    OpenAICompatibleLLM,
    list_models,
    load_config,
    model_factory,
)
from llm.types import LLMError, Message, ToolSpec


README_DIR = CONFIG_DIR


def llm(**overrides) -> OpenAICompatibleLLM:
    kwargs = {"api_key": "k", "base_url": "https://example.invalid/v1", "model": "m"}
    kwargs.update(overrides)
    return OpenAICompatibleLLM(**kwargs)


def completion(**overrides) -> dict:
    payload = {
        "choices": [{"finish_reason": "stop", "message": {"role": "assistant", "content": "好"}}],
        "usage": {"prompt_tokens": 12, "completion_tokens": 3},
    }
    payload.update(overrides)
    return payload


# ---- model_factory ----


@pytest.fixture
def config_dir(tmp_path, monkeypatch):
    monkeypatch.setattr(openai_compatible, "CONFIG_DIR", tmp_path)
    return tmp_path


def write_config(directory: pathlib.Path, name: str, data: dict) -> None:
    (directory / name).write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")


PROVIDER = {"name": "provider_01", "url": "https://nim.invalid/v1", "token": "sk-1"}


def test_model_factory_fills_everything_from_the_config(config_dir):
    write_config(
        config_dir,
        "a.json",
        {
            "providers": [PROVIDER],
            "models": [
                {
                    "id": "Nvidia-Ultra-550B",
                    "display_name": "Furen-max",
                    "provider": "provider_01",
                    "level": "medium",
                    "timeout": 45,
                    "ex_params": {"chat_template_kwargs": {"thinking": True}},
                }
            ],
        },
    )
    built = model_factory("Nvidia-Ultra-550B")
    assert isinstance(built, OpenAICompatibleLLM)
    assert built._api_key == "sk-1"
    assert built._base_url == "https://nim.invalid/v1"
    assert built._model == "Nvidia-Ultra-550B"
    assert built.name == "Furen-max"
    assert built._timeout == 45
    assert built._level == "medium"
    assert built._extra_params == {"chat_template_kwargs": {"thinking": True}}


def test_model_factory_defaults_optional_fields(config_dir):
    write_config(
        config_dir,
        "a.json",
        {"providers": [PROVIDER], "models": [{"id": "wire-id", "provider": "provider_01"}]},
    )
    built = model_factory("wire-id")
    assert built.name == "wire-id"  # 沒有 display_name 就用 id
    assert built._timeout == 60.0
    assert built._level == ""
    assert built._extra_params == {}


def test_model_factory_reports_missing_model_and_provider(config_dir):
    write_config(
        config_dir,
        "a.json",
        {"providers": [PROVIDER], "models": [{"id": "orphan", "provider": "ghost"}]},
    )
    with pytest.raises(LLMError, match="沒有模型 nope"):
        model_factory("nope")
    with pytest.raises(LLMError, match="provider 'ghost' 不存在"):
        model_factory("orphan")


def test_load_config_concatenates_files(config_dir):
    write_config(config_dir, "a.json", {"providers": [PROVIDER], "models": [{"id": "1", "provider": "provider_01"}]})
    write_config(config_dir, "b.json", {"models": [{"id": "2", "provider": "provider_01"}]})
    config = load_config()
    assert [p["name"] for p in config["providers"]] == ["provider_01"]
    assert [m["id"] for m in config["models"]] == ["1", "2"]
    assert model_factory("2")._base_url == PROVIDER["url"]


def test_load_config_rejects_bad_files(config_dir):
    (config_dir / "bad.json").write_text("{nope", encoding="utf-8")
    with pytest.raises(LLMError, match="bad.json"):
        load_config()
    (config_dir / "bad.json").write_text("[]", encoding="utf-8")
    with pytest.raises(LLMError, match="最上層必須是物件"):
        load_config()


def test_list_models_never_leaks_tokens(config_dir):
    write_config(
        config_dir,
        "a.json",
        {
            "providers": [PROVIDER],
            "models": [
                {"id": "a", "display_name": "A 模型", "provider": "provider_01"},
                {"id": "b", "provider": "provider_01"},
            ],
        },
    )
    listed = list_models()
    assert listed == [{"id": "a", "display_name": "A 模型"}, {"id": "b", "display_name": "b"}]
    assert "sk-1" not in json.dumps(listed)


def test_readme_example_builds_furen_max(config_dir):
    # README 裡的範例就是使用者會複製的那段，要能直接用。
    readme = (README_DIR / "README.md").read_text(encoding="utf-8")
    example = readme.split("```json", 1)[1].split("```", 1)[0]
    (config_dir / "provider_config.json").write_text(example, encoding="utf-8")
    built = model_factory("Nvidia-Ultra-550B")
    assert built.name == "Furen-max"
    assert built._level == "medium"
    assert model_factory("Nemotron-3-Super-120B").name == "Furen-large"
    assert [m["display_name"] for m in list_models()] == ["Furen-max", "Furen-large"]


# ---- request ----


def test_build_request_puts_system_first_and_maps_roles():
    request = llm().build_request(
        system="S",
        messages=[
            Message("user", "查一下"),
            Message("assistant", ""),
            Message("tool", "{}", tool_call_id="c1", name="expand_node"),
        ],
    )
    assert request["model"] == "m"
    assert request["messages"][0] == {"role": "system", "content": "S"}
    assert request["messages"][1] == {"role": "user", "content": "查一下"}
    # 空的 assistant 什麼都不補，接著就是 tool 結果。
    assert request["messages"][2] == {"role": "tool", "tool_call_id": "c1", "content": "{}"}
    assert request["max_tokens"] == 4096
    assert "tools" not in request
    assert "response_format" not in request
    assert "reasoning_effort" not in request


def test_build_request_omits_empty_system():
    request = llm().build_request(system="", messages=[Message("user", "hi")])
    assert request["messages"] == [{"role": "user", "content": "hi"}]


def test_build_request_tools_schema_level_and_extra_params():
    request = llm(level="high", extra_params={"temperature": 0, "max_tokens": 9}).build_request(
        system="",
        messages=[Message("user", "hi")],
        tools=[ToolSpec("expand_node", "展開節點", {"type": "object"})],
        schema={"type": "object"},
        max_tokens=99,
    )
    assert request["tools"] == [
        {
            "type": "function",
            "function": {
                "name": "expand_node",
                "description": "展開節點",
                "parameters": {"type": "object"},
            },
        }
    ]
    assert request["response_format"] == {
        "type": "json_schema",
        "json_schema": {"name": "reply", "schema": {"type": "object"}},
    }
    assert request["reasoning_effort"] == "high"
    assert request["temperature"] == 0
    # ex_params 最後蓋上去，可以壓過我們自己排的鍵。
    assert request["max_tokens"] == 9


# ---- reply ----


def test_parse_reply_reads_text_and_usage():
    reply = llm().parse_reply(completion())
    assert reply.text == "好"
    assert reply.stop_reason == "stop"
    assert reply.tool_calls == ()
    assert (reply.usage.input_tokens, reply.usage.output_tokens) == (12, 3)


def test_parse_reply_parses_string_tool_call_arguments():
    reply = llm().parse_reply(
        completion(
            choices=[
                {
                    "finish_reason": "tool_calls",
                    "message": {
                        "role": "assistant",
                        "content": None,
                        "tool_calls": [
                            {
                                "id": "call_1",
                                "type": "function",
                                "function": {"name": "expand_node", "arguments": '{"address": "TXabc"}'},
                            }
                        ],
                    },
                }
            ]
        )
    )
    assert reply.stop_reason == "tool_calls"
    assert reply.text == ""
    assert reply.tool_calls[0].id == "call_1"
    assert reply.tool_calls[0].name == "expand_node"
    assert reply.tool_calls[0].arguments == {"address": "TXabc"}
    assert reply.provider_steps == ()  # 沒有簽章要回放，續跑靠 format_assistant 重建


def test_parse_reply_maps_finish_reasons():
    truncated = completion(choices=[{"finish_reason": "length", "message": {"content": "半"}}])
    assert llm().parse_reply(truncated).stop_reason == "length"
    refused = completion(choices=[{"finish_reason": "content_filter", "message": {"content": ""}}])
    assert llm().parse_reply(refused).stop_reason == "refusal"


def test_parse_reply_reads_reasoning_tokens():
    reply = llm().parse_reply(
        completion(
            usage={
                "prompt_tokens": 1,
                "completion_tokens": 20,
                "completion_tokens_details": {"reasoning_tokens": 15},
            }
        )
    )
    assert reply.usage.thought_tokens == 15


def test_parse_reply_raises_on_error_body_and_missing_choices():
    with pytest.raises(LLMError, match="OpenAI 回覆失敗"):
        llm().parse_reply({"error": {"message": "boom"}})
    with pytest.raises(LLMError, match="沒有 choices"):
        llm().parse_reply({"choices": []})


def test_constructing_the_adapter_does_not_need_the_sdk():
    built = llm()
    assert built._client is None
    assert "openai" not in sys.modules
