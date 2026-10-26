"""Claim 抽取的請求／回應契約。沒有自由文字欄位。"""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator


class ClaimIn(BaseModel):
    model_config = ConfigDict(extra="forbid")

    kind: Literal["address", "tx_hash", "amount"]
    value: str
    role: Literal["from", "to", "unknown"] | None = None
    asset: Literal["USDT"] | None = None
    confidence: float = Field(ge=0.0, le=1.0)

    @model_validator(mode="after")
    def validate_by_kind(self) -> "ClaimIn":
        if self.kind == "address":
            if self.role is None:
                raise ValueError("address claims require role")
            self.asset = None
        elif self.kind == "tx_hash":
            self.role = None
            self.asset = None
        elif self.kind == "amount":
            if self.asset != "USDT":
                raise ValueError("amount claims require asset USDT")
            self.role = None
        return self


class ExtractClaimsRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    image_base64: str
    mime_type: Literal["image/png"] = "image/png"


class ExtractClaimsResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    network: Literal["tron"]
    claims: list[ClaimIn] = Field(default_factory=list, max_length=10)
