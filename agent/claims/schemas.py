"""Claim 抽取的請求／回應契約。沒有自由文字欄位。"""

from typing import Literal

from pydantic import BaseModel, Field


class ClaimIn(BaseModel):
    kind: Literal["address"]
    value: str
    role: Literal["from", "to", "unknown"]
    confidence: float = Field(ge=0.0, le=1.0)


class ExtractClaimsRequest(BaseModel):
    image_base64: str
    mime_type: Literal["image/png"] = "image/png"


class ExtractClaimsResponse(BaseModel):
    network: Literal["tron"]
    claims: list[ClaimIn]
