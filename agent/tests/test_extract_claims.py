"""POST /v1/extract-claims：FakeLLM、無網路、無金鑰。"""

import base64
import json

from fastapi.testclient import TestClient

from app import create_app
from llm.fake import FakeLLM
from llm.types import Reply, ToolCall
from session import SessionStore

KEY = "test-key"
HEADERS = {"X-Agent-Key": KEY}
VALID_ADDRESS = "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj"
# 1×1 PNG
PNG_BYTES = bytes.fromhex(
    "89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c489"
    "0000000a49444154789c63000100000500010d0a2db40000000049454e44ae426082"
)


def build(replies, monkeypatch):
    monkeypatch.setenv("AGENT_SHARED_KEY", KEY)
    llm = FakeLLM(replies)
    return TestClient(create_app(llm=llm, store=SessionStore())), llm


def png_payload(**overrides):
    body = {
        "image_base64": base64.b64encode(PNG_BYTES).decode("ascii"),
        "mime_type": "image/png",
    }
    body.update(overrides)
    return body


def claim_reply(address=VALID_ADDRESS, role="to", confidence=0.91):
    return Reply(
        text=json.dumps(
            {
                "network": "tron",
                "claims": [
                    {
                        "kind": "address",
                        "value": address,
                        "role": role,
                        "confidence": confidence,
                    }
                ],
            }
        )
    )


def test_extract_rejects_missing_key(monkeypatch):
    client, _ = build([claim_reply()], monkeypatch)
    assert client.post("/v1/extract-claims", json=png_payload()).status_code == 401


def test_extract_returns_one_pending_address_claim(monkeypatch):
    client, llm = build([claim_reply()], monkeypatch)
    response = client.post("/v1/extract-claims", json=png_payload(), headers=HEADERS)
    assert response.status_code == 200
    body = response.json()
    assert body == {
        "network": "tron",
        "claims": [
            {
                "kind": "address",
                "value": VALID_ADDRESS,
                "role": "to",
                "confidence": 0.91,
            }
        ],
    }
    assert len(llm.calls) == 1
    assert llm.calls[0]["tools"] == []
    assert llm.calls[0]["schema"] is not None
    message = llm.calls[0]["messages"][0]
    assert message.images and message.images[0].mime_type == "image/png"
    assert message.images[0].data.startswith(b"\x89PNG")


def test_extract_rejects_tool_calls(monkeypatch):
    client, _ = build(
        [Reply(tool_calls=(ToolCall("fc_1", "expand_node", {}),), stop_reason="tool_calls")],
        monkeypatch,
    )
    response = client.post("/v1/extract-claims", json=png_payload(), headers=HEADERS)
    assert response.status_code == 400
    assert response.json()["code"] == "malformed_claims"


def test_extract_rejects_invalid_tron_address(monkeypatch):
    client, _ = build([claim_reply(address="not-a-tron-address")], monkeypatch)
    response = client.post("/v1/extract-claims", json=png_payload(), headers=HEADERS)
    assert response.status_code == 400
    assert response.json()["code"] == "invalid_claim_address"


def test_extract_rejects_non_png_bytes(monkeypatch):
    client, _ = build([claim_reply()], monkeypatch)
    response = client.post(
        "/v1/extract-claims",
        json={
            "image_base64": base64.b64encode(b"not-a-png").decode("ascii"),
            "mime_type": "image/png",
        },
        headers=HEADERS,
    )
    assert response.status_code == 400
    assert response.json()["code"] == "invalid_image"


def test_is_valid_tron_address_matches_known_fixtures():
    from tron_address import is_valid_tron_address

    assert is_valid_tron_address(VALID_ADDRESS)
    assert is_valid_tron_address("T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb")
    assert not is_valid_tron_address("TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdk")
    assert not is_valid_tron_address("")
