"""訓練資料的讀取。

這裡測的是一個真的踩過的坑：爬蟲用 append 模式寫檔，所以每執行一次就是一個
獨立的 gzip 段。只要有一段被中斷，`gzip.open` 讀到它就整個放棄 —— 實測有個
24 MB、裡面裝著 134 MB 完整資料的檔案，被判成空的，續爬因此整批重爬了一次。
"""

import gzip
import json
import pathlib
import shutil
import tempfile
import zlib

import pytest

from scoring.train.dataset import read_records


@pytest.fixture
def tmp_path():
    r"""自建暫存目錄，不用 pytest 的 tmp_path。

    這台機器上 %TEMP%\pytest-of-<user> 是拒絕存取的，pytest 的 fixture 會在
    setup 就炸。tempfile 直接開則沒事，測試不該卡在這種環境細節上。
    """
    directory = pathlib.Path(tempfile.mkdtemp())
    yield directory
    shutil.rmtree(directory, ignore_errors=True)


def member(records) -> bytes:
    """把幾筆記錄壓成一個完整的 gzip 段。"""
    body = "".join(json.dumps(r, ensure_ascii=False) + "\n" for r in records)
    return gzip.compress(body.encode("utf-8"))


def record(address: str) -> dict:
    return {"address": address, "created_at": None, "truncated": False, "transfers": []}


def test_reads_a_single_healthy_member(tmp_path):
    path = tmp_path / "transfers.jsonl.gz"
    path.write_bytes(member([record("TA"), record("TB")]))

    assert [r["address"] for r in read_records(path)] == ["TA", "TB"]


def test_a_truncated_first_member_does_not_hide_the_healthy_ones(tmp_path):
    # 這就是那個 bug：gzip.open 在這種檔案上回傳零筆。
    path = tmp_path / "transfers.jsonl.gz"
    cut = member([record("TLost")])[:-6]
    path.write_bytes(cut + member([record("TKept1"), record("TKept2")]))

    with pytest.raises((EOFError, OSError, ValueError, zlib.error)):
        with gzip.open(path, "rt", encoding="utf-8") as handle:
            handle.read()

    assert [r["address"] for r in read_records(path)] == ["TKept1", "TKept2"]


def test_a_half_written_final_line_costs_one_record_not_the_file(tmp_path):
    path = tmp_path / "transfers.jsonl.gz"
    body = json.dumps(record("TGood")) + "\n" + '{"address": "THalf", "transf'
    path.write_bytes(gzip.compress(body.encode("utf-8")))

    assert [r["address"] for r in read_records(path)] == ["TGood"]


def test_several_appended_runs_all_come_back(tmp_path):
    path = tmp_path / "transfers.jsonl.gz"
    path.write_bytes(member([record("T1")]) + member([record("T2")]) + member([record("T3")]))

    assert [r["address"] for r in read_records(path)] == ["T1", "T2", "T3"]
