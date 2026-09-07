"""POST /v1/score 的線上格式。

跟 agent/schemas.py（/v1/agent/chat 的合約）分開：那是 LLM 那側的事，這裡是
scoring 自己的，兩邊互不 import（見 test_scoring_boundary.py）。
"""

from pydantic import BaseModel, Field


class TransferIn(BaseModel):
    from_address: str
    to_address: str
    amount: str
    """最小單位的整數，字串型式 —— 絕對不能是裸的 JSON 數字。2**53 以上的金額
    一旦被當成 JSON number 解析就會悄悄失真，跟 features.py 開頭那條規則是同一件事。"""
    timestamp_ms: int


class ScoreRequest(BaseModel):
    target_address: str
    window_start_ms: int
    window_end_ms: int
    truncated: bool = False
    transfers: list[TransferIn] = Field(default_factory=list)


class ScoreResponse(BaseModel):
    score: float
    model_version: str
    """model/manifest.json 的 trainingDataHash。Phase 4 拿它分辨一筆分數是哪個
    訓練出來的模型打的，manifest 裡其他欄位都已經跟著 model.joblib 存了一份，
    不必每次打分都重複回傳。"""
