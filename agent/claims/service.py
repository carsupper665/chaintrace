"""從截圖抽出單一 TRON 地址 Claim。

不做 Tool Call、不連 DB、不驗 Owner。Go 把圖片 bytes 送來，這裡只負責
組 prompt、呼叫 LLM 結構化輸出、再用與 Go 相同的 Base58Check 過濾。
"""

from __future__ import annotations

import base64
import json
import pathlib
from typing import Any

from llm.types import ImageContent, LLMError, Message, Reply
from tron_address import is_valid_tron_address

from .schemas import ClaimIn, ExtractClaimsRequest, ExtractClaimsResponse

PROMPT_PATH = pathlib.Path(__file__).resolve().parent.parent / "prompts" / "extract_claims.md"

CLAIM_SCHEMA: dict[str, Any] = {
    "type": "object",
    "additionalProperties": False,
    "required": ["network", "claims"],
    "properties": {
        "network": {"type": "string", "const": "tron"},
        "claims": {
            "type": "array",
            "minItems": 1,
            "maxItems": 1,
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": ["kind", "value", "role", "confidence"],
                "properties": {
                    "kind": {"type": "string", "const": "address"},
                    "value": {"type": "string"},
                    "role": {"type": "string", "enum": ["from", "to", "unknown"]},
                    "confidence": {"type": "number", "minimum": 0, "maximum": 1},
                },
            },
        },
    },
}


class ExtractError(ValueError):
    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code
        self.message = message


def _system_prompt() -> str:
    text = PROMPT_PATH.read_text(encoding="utf-8").strip()
    if not text:
        raise ExtractError("internal_error", "extract prompt is empty")
    return text


def _decode_image(payload: ExtractClaimsRequest) -> bytes:
    try:
        raw = base64.b64decode(payload.image_base64, validate=True)
    except Exception as error:  # noqa: BLE001 - 任何解碼失敗都是同一種輸入錯誤
        raise ExtractError("invalid_image", "image_base64 is not valid base64") from error
    if not raw:
        raise ExtractError("invalid_image", "image is empty")
    if not raw.startswith(b"\x89PNG\r\n\x1a\n"):
        raise ExtractError("invalid_image", "only PNG is accepted")
    return raw


def _parse_reply(reply: Reply) -> ExtractClaimsResponse:
    if reply.tool_calls:
        raise ExtractError("malformed_claims", "extract must not return tool calls")
    try:
        raw = json.loads(reply.text)
    except ValueError as error:
        raise ExtractError("malformed_claims", "extract reply is not JSON") from error
    if not isinstance(raw, dict):
        raise ExtractError("malformed_claims", "extract reply must be an object")
    try:
        parsed = ExtractClaimsResponse.model_validate(raw)
    except Exception as error:  # noqa: BLE001 - pydantic 細節不外洩給呼叫端
        raise ExtractError("malformed_claims", "extract reply failed schema validation") from error
    if len(parsed.claims) != 1:
        raise ExtractError("malformed_claims", "extract must return exactly one claim")
    claim = parsed.claims[0]
    if claim.kind != "address" or not is_valid_tron_address(claim.value):
        raise ExtractError("invalid_claim_address", "extracted address failed Base58Check")
    return ExtractClaimsResponse(
        network="tron",
        claims=[
            ClaimIn(
                kind="address",
                value=claim.value,
                role=claim.role,
                confidence=claim.confidence,
            )
        ],
    )


def extract_claims(llm, payload: ExtractClaimsRequest) -> ExtractClaimsResponse:
    image = _decode_image(payload)
    try:
        reply = llm.complete(
            system=_system_prompt(),
            messages=(
                Message(
                    role="user",
                    content="Extract the single TRON address claim from this screenshot.",
                    images=(ImageContent(data=image, mime_type=payload.mime_type),),
                ),
            ),
            tools=(),
            schema=CLAIM_SCHEMA,
            max_tokens=512,
        )
    except LLMError as error:
        raise ExtractError("llm_error", str(error)) from error
    return _parse_reply(reply)
