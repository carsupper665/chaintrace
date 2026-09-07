"""Address Feature Vector 打分服務 —— POST /v1/score。

跟 /v1/agent/chat 用同一把 X-Agent-Key 檢查（見 agent/app.py 開頭的邊界說明：
共用密鑰只是確認來源是 Go，不是授權機制），這裡不另外做授權判斷。

模型在這個模組被 import 時載入一次，不是每個請求都重讀 model.joblib
（見 docs/adr/0015：訓練與線上打分共用 features.py，這裡共用的是訓練出來的
scaler/model 本身）。
"""

import json
import pathlib
from datetime import datetime, timezone

import joblib
import numpy as np
from fastapi import APIRouter, Header, Request
from fastapi.responses import JSONResponse

from scoring.features import FEATURE_NAMES, Transfer, compute, vector
from scoring.schemas import ScoreRequest, ScoreResponse, TransferIn

MODEL_DIR = pathlib.Path(__file__).resolve().parent / "model"

_artifact = joblib.load(MODEL_DIR / "model.joblib")
_scaler = _artifact["scaler"]
_model = _artifact["model"]
_keep = _artifact["keep"]

_manifest = json.loads((MODEL_DIR / "manifest.json").read_text(encoding="utf-8"))
MODEL_VERSION = _manifest["trainingDataHash"]

if tuple(_artifact["features"]) != FEATURE_NAMES:
    # features.py 改過順序或增刪欄位，但沒有重新訓練模型：硬停在啟動時，好過
    # 線上悄悄拿「轉帳筆數」當「金額中位數」算，照樣吐出一個看起來正常的分數。
    raise RuntimeError(
        "model.joblib was trained on a different feature list than features.py "
        "currently defines; retrain via scoring/train/train.py before serving"
    )

router = APIRouter()


def _moment(milliseconds: int) -> datetime:
    return datetime.fromtimestamp(milliseconds / 1000, tz=timezone.utc)


def _to_transfer(transfer: TransferIn) -> Transfer:
    return Transfer(
        from_address=transfer.from_address,
        to_address=transfer.to_address,
        amount=int(transfer.amount),
        timestamp=_moment(transfer.timestamp_ms),
    )


def compute_vector(payload: ScoreRequest) -> tuple[float, ...]:
    """一個請求變成一個特徵向量 —— 跟訓練那邊的 features.compute()+vector() 是
    同一份程式碼，只是輸入從讀檔換成請求本文。train/serve 一致性測試比對的
    就是這個函式的輸出跟離線算出來的向量是否逐欄相同。"""
    transfers = [_to_transfer(t) for t in payload.transfers]
    features = compute(
        transfers,
        payload.target_address,
        window_start=_moment(payload.window_start_ms),
        window_end=_moment(payload.window_end_ms),
        truncated=payload.truncated,
        decimals=payload.decimals,
    )
    return vector(features)


def score_request(payload: ScoreRequest) -> float:
    """路由的唯一入口：一個請求變成一個分數。"""
    matrix = np.asarray([compute_vector(payload)], dtype=float)[:, _keep]
    return float(-_model.score_samples(_scaler.transform(matrix))[0])


@router.post("/v1/score")
def score(
    payload: ScoreRequest, request: Request, x_agent_key: str = Header(default="")
) -> JSONResponse:
    shared_key = getattr(request.app.state, "shared_key", "")
    if not shared_key or x_agent_key != shared_key:
        return JSONResponse(
            status_code=401, content={"code": "unauthorized", "message": "Invalid agent key"}
        )
    response = ScoreResponse(score=score_request(payload), model_version=MODEL_VERSION)
    return JSONResponse(status_code=200, content=response.model_dump())
