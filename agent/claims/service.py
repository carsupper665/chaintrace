"""從截圖抽出 bounded Claim 集合。

不做 Tool Call、不連 DB、不驗 Owner。Go 把已消毒的 PNG bytes 送來，這裡只負責
組 prompt、呼叫 LLM 結構化輸出、再用與 Go 相同的格式規則過濾。
"""

from __future__ import annotations

import base64
import json
import pathlib
import re
from typing import Any

from llm.types import ImageContent, LLMError, Message, Reply
from tron_address import is_valid_tron_address

from .schemas import ClaimIn, ExtractClaimsRequest, ExtractClaimsResponse

PROMPT_PATH = pathlib.Path(__file__).resolve().parent.parent / "prompts" / "extract_claims.md"
TX_HASH = re.compile(r"^[0-9a-fA-F]{64}$")
AMOUNT_PATTERN = re.compile(r"^[0-9]+$")
MAX_CLAIMS = 10

# OpenAI / Codex structured outputs require every property key to appear in
# `required`. Optional claim fields therefore use an explicit null alternative.
CLAIM_SCHEMA: dict[str, Any] = {
    "type": "object",
    "additionalProperties": False,
    "required": ["network", "claims"],
    "properties": {
        "network": {"type": "string", "enum": ["tron"]},
        "claims": {
            "type": "array",
            "minItems": 0,
            "maxItems": MAX_CLAIMS,
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": ["kind", "value", "role", "asset", "confidence"],
                "properties": {
                    "kind": {"type": "string", "enum": ["address", "tx_hash", "amount"]},
                    "value": {"type": "string"},
                    "role": {
                        "anyOf": [
                            {"type": "string", "enum": ["from", "to", "unknown"]},
                            {"type": "null"},
                        ]
                    },
                    "asset": {
                        "anyOf": [
                            {"type": "string", "enum": ["USDT"]},
                            {"type": "null"},
                        ]
                    },
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
    except Exception as error:  # noqa: BLE001
        raise ExtractError("invalid_image", "image_base64 is not valid base64") from error
    if not raw:
        raise ExtractError("invalid_image", "image is empty")
    if not raw.startswith(b"\x89PNG\r\n\x1a\n"):
        raise ExtractError("invalid_image", "only PNG is accepted")
    return raw


def _validate_claim(claim: ClaimIn) -> ClaimIn:
    if claim.kind == "address":
        if claim.role is None or not is_valid_tron_address(claim.value):
            raise ExtractError("invalid_claim_address", "extracted address failed Base58Check")
        return ClaimIn(
            kind="address", value=claim.value, role=claim.role, confidence=claim.confidence
        )
    if claim.kind == "tx_hash":
        if not TX_HASH.fullmatch(claim.value):
            raise ExtractError("malformed_claims", "invalid transaction hash")
        return ClaimIn(kind="tx_hash", value=claim.value.lower(), confidence=claim.confidence)
    if claim.kind == "amount":
        if claim.asset != "USDT" or not AMOUNT_PATTERN.fullmatch(claim.value):
            raise ExtractError("malformed_claims", "invalid amount claim")
        return ClaimIn(
            kind="amount", value=claim.value, asset="USDT", confidence=claim.confidence
        )
    raise ExtractError("malformed_claims", "unknown claim kind")


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
    except Exception as error:  # noqa: BLE001
        raise ExtractError("malformed_claims", "extract reply failed schema validation") from error
    if len(parsed.claims) == 0:
        raise ExtractError("no_claim", "screenshot contained no extractable claim")
    if len(parsed.claims) > MAX_CLAIMS:
        raise ExtractError("malformed_claims", "too many claims")
    claims = [_validate_claim(claim) for claim in parsed.claims]
    return ExtractClaimsResponse(network="tron", claims=claims)


def extract_claims(llm, payload: ExtractClaimsRequest) -> ExtractClaimsResponse:
    image = _decode_image(payload)
    try:
        reply = llm.complete(
            system=_system_prompt(),
            messages=(
                Message(
                    role="user",
                    content="Extract TRON claims from this screenshot.",
                    images=(ImageContent(data=image, mime_type=payload.mime_type),),
                ),
            ),
            tools=(),
            schema=CLAIM_SCHEMA,
            max_tokens=1024,
        )
    except LLMError as error:
        raise ExtractError("llm_error", str(error)) from error
    return _parse_reply(reply)
