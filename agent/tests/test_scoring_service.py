"""scoring/service.py 的 HTTP 層測試，外加 train/serve 一致性驗證
（docs/learned-risk-scoring-plan.md Phase 3 驗收 #4）。"""

import json
import math
import pathlib

from fastapi.testclient import TestClient

from app import create_app
from llm.fake import FakeLLM
from scoring import service
from scoring.features import FEATURE_NAMES, Transfer, compute, vector
from scoring.schemas import ScoreRequest, TransferIn
from session import SessionStore

KEY = "test-key"
HEADERS = {"X-Agent-Key": KEY}

FIXTURE = json.loads(
    (pathlib.Path(__file__).resolve().parent / "testdata" / "parity_transfers.json").read_text(
        encoding="utf-8"
    )
)


def build(monkeypatch) -> TestClient:
    monkeypatch.setenv("AGENT_SHARED_KEY", KEY)
    return TestClient(create_app(llm=FakeLLM([]), store=SessionStore()))


def fixture_request() -> ScoreRequest:
    return ScoreRequest(
        target_address=FIXTURE["address"],
        window_start_ms=FIXTURE["windowStartMs"],
        window_end_ms=FIXTURE["windowEndMs"],
        truncated=FIXTURE["truncated"],
        transfers=[
            TransferIn(
                from_address=t["from"],
                to_address=t["to"],
                amount=t["value"],
                timestamp_ms=t["ts"],
            )
            for t in FIXTURE["transfers"]
        ],
    )


def test_model_artifact_matches_declared_features():
    assert tuple(service._artifact["features"]) == FEATURE_NAMES


def test_train_serve_parity_for_one_real_address():
    """離線直接用 features.compute() 算一次，跟送進一個 ScoreRequest 用
    service.compute_vector() 算一次，兩邊的向量要逐欄一模一樣 —— 這是
    train/serve 一致性檢查字面上的意思，不是比對最終分數。"""
    offline_transfers = [
        Transfer(
            from_address=t["from"],
            to_address=t["to"],
            amount=int(t["value"]),
            timestamp=service._moment(t["ts"]),
        )
        for t in FIXTURE["transfers"]
    ]
    offline = vector(
        compute(
            offline_transfers,
            FIXTURE["address"],
            window_start=service._moment(FIXTURE["windowStartMs"]),
            window_end=service._moment(FIXTURE["windowEndMs"]),
            truncated=FIXTURE["truncated"],
        )
    )
    online = service.compute_vector(fixture_request())
    for name, offline_value, online_value in zip(FEATURE_NAMES, offline, online):
        assert offline_value == online_value, f"{name}: offline={offline_value} online={online_value}"


def test_score_route_end_to_end(monkeypatch):
    client = build(monkeypatch)
    response = client.post("/v1/score", json=fixture_request().model_dump(), headers=HEADERS)
    assert response.status_code == 200
    body = response.json()
    assert math.isfinite(body["score"])
    assert body["model_version"] == service.MODEL_VERSION


def test_missing_key_is_rejected(monkeypatch):
    client = build(monkeypatch)
    response = client.post("/v1/score", json=fixture_request().model_dump())
    assert response.status_code == 401


def test_wrong_key_is_rejected(monkeypatch):
    client = build(monkeypatch)
    response = client.post(
        "/v1/score", json=fixture_request().model_dump(), headers={"X-Agent-Key": "nope"}
    )
    assert response.status_code == 401
    assert response.json()["code"] == "unauthorized"


def test_malformed_payload_is_rejected(monkeypatch):
    client = build(monkeypatch)
    response = client.post("/v1/score", json={"target_address": "T..."}, headers=HEADERS)
    assert response.status_code == 422
