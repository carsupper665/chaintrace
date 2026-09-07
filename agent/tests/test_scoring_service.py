"""scoring/service.py 的 HTTP 層測試，外加 train/serve 一致性驗證
（docs/learned-risk-scoring-plan.md Phase 3 驗收 #4）。"""

import gzip
import json
import math
import pathlib
import shutil
import tempfile

import pytest
from fastapi.testclient import TestClient

from app import create_app
from llm.fake import FakeLLM
from scoring import service
from scoring.features import FEATURE_NAMES
from scoring.schemas import ScoreRequest, TransferIn
from scoring.train import dataset
from session import SessionStore

KEY = "test-key"
HEADERS = {"X-Agent-Key": KEY}

FIXTURE = json.loads(
    (pathlib.Path(__file__).resolve().parent / "testdata" / "parity_transfers.json").read_text(
        encoding="utf-8"
    )
)


@pytest.fixture
def tmp_path():
    r"""自建暫存目錄，理由同 test_dataset_reader.py：這台機器上
    %TEMP%\pytest-of-<user> 是拒絕存取的，pytest 的 fixture 會在 setup 就炸。"""
    directory = pathlib.Path(tempfile.mkdtemp())
    yield directory
    shutil.rmtree(directory, ignore_errors=True)


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


def build_training_directory(directory: pathlib.Path) -> pathlib.Path:
    """把 fixture 寫成爬蟲產出的樣子，好讓離線那側走真正的訓練讀取路徑。"""
    directory.mkdir(parents=True, exist_ok=True)
    (directory / "manifest.json").write_text(
        json.dumps(
            {
                "windowStartMs": FIXTURE["windowStartMs"],
                "windowEndMs": FIXTURE["windowEndMs"],
                "windowDays": 30,
                "transferCap": 10000,
            }
        ),
        encoding="utf-8",
    )
    record = {
        "address": FIXTURE["address"],
        "truncated": FIXTURE["truncated"],
        "transfers": FIXTURE["transfers"],
    }
    body = json.dumps(record, ensure_ascii=False) + "\n"
    (directory / "transfers.jsonl.gz").write_bytes(gzip.compress(body.encode("utf-8")))
    return directory


def test_model_artifact_matches_declared_features():
    assert tuple(service._artifact["features"]) == FEATURE_NAMES


def test_train_serve_parity_for_one_real_address(tmp_path):
    """離線走 scoring/train/dataset.py 真正的讀取路徑，線上走請求的路徑，
    兩邊算出來的向量要逐欄一模一樣。

    兩側如果都直接呼叫 features.compute()，這個測試就永遠不會失敗 —— 它要比的
    是兩條真的不同的程式碼路徑，不是同一個函式的兩次呼叫。
    """
    addresses, rows = dataset.load(build_training_directory(tmp_path / "train-data"))
    assert addresses == [FIXTURE["address"]]
    offline = tuple(rows[0])

    online = service.compute_vector(fixture_request())

    assert len(offline) == len(FEATURE_NAMES)
    for name, offline_value, online_value in zip(FEATURE_NAMES, offline, online):
        assert offline_value == online_value, f"{name}: offline={offline_value} online={online_value}"


def test_an_amount_above_2_53_survives_the_wire():
    """9007199254740993 是 2^53 + 1。JSON number 到這裡就開始失真，所以金額走
    字串 —— features.py 開頭那條「金額全程用 int」規則的線上對應。"""
    transfer = service._to_transfer(
        TransferIn(
            from_address="TA", to_address="TB", amount="9007199254740993", timestamp_ms=0
        )
    )
    assert transfer.amount == 9007199254740993


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
