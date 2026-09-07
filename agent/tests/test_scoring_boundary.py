"""ADR-0015 的邊界：scoring 與 LLM 兩側互不相欠。

同一個 process 沒關係，共用 import 就有關係。把 scoring/ 整個刪掉，agent 應該
照樣啟動，只是少一條路由；把 llm/ 換掉，scoring 也不該有感覺。

用 AST 靜態掃描而不是真的 import：檢查的是「原始碼寫了什麼」，不會因為某個模組
剛好還沒被載入就漏掉。
"""

import ast
import pathlib

import pytest

AGENT = pathlib.Path(__file__).resolve().parent.parent
SCORING = AGENT / "scoring"
TRAIN = SCORING / "train"

# LLM 那一側的模組。scoring 不准碰。
LLM_SIDE = ("llm", "prompt", "session", "turn", "schemas")

# features.py 必須是純函式：不連網、不碰資料庫、不載入模型或框架。
FORBIDDEN_IN_FEATURES = (
    "httpx",
    "requests",
    "urllib",
    "http",
    "socket",
    "sqlalchemy",
    "sqlite3",
    "sklearn",
    "joblib",
    "pickle",
    "numpy",
    "pandas",
    "fastapi",
    "pydantic",
)


def imported_modules(path: pathlib.Path) -> set[str]:
    """檔案裡所有絕對 import 的完整模組名。

    相對 import（`from .features import ...`）是套件內部的事，不算跨界。
    """
    tree = ast.parse(path.read_text(encoding="utf-8"))
    modules: set[str] = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            modules.update(alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
            modules.add(node.module)
    return modules


def touches(modules: set[str], prefix: str) -> set[str]:
    return {m for m in modules if m == prefix or m.startswith(prefix + ".")}


def server_files() -> list[pathlib.Path]:
    """伺服器實際會載入的檔案。tests/ 不算，測試碰兩邊是應該的。"""
    return [p for p in AGENT.glob("*.py") if p.name != "smoke.py"] + sorted(
        (AGENT / "llm").glob("*.py")
    )


def scoring_files() -> list[pathlib.Path]:
    return sorted(SCORING.rglob("*.py"))


@pytest.mark.parametrize("path", scoring_files(), ids=lambda p: p.name)
def test_scoring_never_imports_the_llm_side(path):
    modules = imported_modules(path)
    for side in LLM_SIDE:
        assert not touches(modules, side), f"{path.name} imports {side}"


@pytest.mark.parametrize(
    "path", [p for p in server_files() if p.name != "app.py"], ids=lambda p: p.name
)
def test_the_llm_side_never_imports_scoring(path):
    # 這是「刪掉 scoring/ 仍然開得起來」的靜態證明。app.py 是計畫裡指定的接點
    # （docs/learned-risk-scoring-plan.md、ADR-0015），它 import scoring.service
    # 是刻意的，用下面 test_app_imports_scoring_defensively 顧著這件事該有的樣子。
    assert not touches(imported_modules(path), "scoring"), f"{path.name} imports scoring"


def _guards_a_scoring_import(node: ast.Try) -> bool:
    imports_scoring = any(
        isinstance(child, ast.ImportFrom) and child.module and touches({child.module}, "scoring")
        for statement in node.body
        for child in ast.walk(statement)
    )
    catches_import_error = any(
        handler.type is None
        or (isinstance(handler.type, ast.Name) and handler.type.id == "ImportError")
        for handler in node.handlers
    )
    return imports_scoring and catches_import_error


def test_app_imports_scoring_defensively():
    """app.py 對 scoring 的 import 必須包在 try/except ImportError 裡：

    刪掉整個 scoring/ 目錄時，這個 import 要失敗得安靜，app 才能照樣啟動、
    只是少一條路由（Phase 3 驗收條件）。裸的 import 會讓刪除變成啟動時崩潰。
    """
    tree = ast.parse((AGENT / "app.py").read_text(encoding="utf-8"))
    guarded = any(
        _guards_a_scoring_import(node) for node in ast.walk(tree) if isinstance(node, ast.Try)
    )
    assert guarded, "app.py must import scoring inside a try/except ImportError"


@pytest.mark.parametrize("path", server_files() + scoring_files(), ids=lambda p: p.name)
def test_nothing_here_imports_the_standalone_crawler(path):
    # crawler/ 是獨立部署單元，會被複製到別台機器上跑。讓它進 agent 的 import
    # graph，這個「複製整個資料夾就能跑」的性質就沒了。
    assert not touches(imported_modules(path), "crawler"), f"{path.name} imports crawler"


@pytest.mark.parametrize(
    "path", [p for p in scoring_files() if TRAIN not in p.parents], ids=lambda p: p.name
)
def test_the_served_half_of_scoring_never_imports_the_trainer(path):
    # train/ 只在離線跑，而且拖著 numpy 和 scikit-learn。讓它進伺服器的 import
    # graph，agent 就開始扛訓練用的依賴。
    assert not touches(imported_modules(path), "scoring.train"), f"{path.name} imports train"


def test_features_stays_a_pure_function_module():
    modules = imported_modules(SCORING / "features.py")
    for forbidden in FORBIDDEN_IN_FEATURES:
        assert not touches(modules, forbidden), f"features.py imports {forbidden}"
