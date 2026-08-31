"""ChainTrace LLM Agent —— 只綁內網的 sidecar。

邊界（見 docs/development-rules.md 第 2、5 節）：
  不連資料庫、不驗身分、不判斷 Owner 權限、不算分數。
  Go 打過來的請求一律視為已授權；共用密鑰只是確認來源是 Go，不是授權機制。
"""

import os
import pathlib

from fastapi import FastAPI, Header
from fastapi.responses import JSONResponse

from llm import LLMError, from_env
from prompt import verify_prompts_present
from schemas import ChatRequest, ChatResponse
from session import SessionStore
from turn import InvalidTurn, SessionExpired, run_turn

ENV_FILE = pathlib.Path(__file__).resolve().parent / ".env"


def load_env() -> None:
    """讀取 agent/.env。作業系統既有的環境變數優先，跟 Go 那側的行為一致。"""
    if not ENV_FILE.exists():
        return
    from dotenv import load_dotenv

    load_dotenv(ENV_FILE, override=False)


def _int_env(key: str, default: int) -> int:
    try:
        return int(os.getenv(key, "").strip() or default)
    except ValueError:
        return default


def create_app(*, llm=None, store: SessionStore | None = None) -> FastAPI:
    """llm 與 store 可注入，測試就不必碰網路。"""
    verify_prompts_present()

    app = FastAPI(title="ChainTrace Agent", docs_url=None, redoc_url=None)
    app.state.llm = from_env() if llm is None else llm
    # 空的 SessionStore 是 falsy（它有 __len__），所以這裡只能比 None，
    # 用 `store or ...` 會把呼叫端傳進來的 store 悄悄丟掉。
    app.state.store = (
        SessionStore(
            ttl_seconds=_int_env("SESSION_TTL_SECONDS", 1800),
            max_sessions=_int_env("SESSION_MAX", 200),
        )
        if store is None
        else store
    )
    app.state.shared_key = os.getenv("AGENT_SHARED_KEY", "").strip()
    app.state.max_tokens = _int_env("LLM_MAX_TOKENS", 4096)

    @app.get("/healthz")
    def healthz() -> dict:
        return {"status": "ok", "sessions": len(app.state.store)}

    @app.post("/v1/agent/chat")
    def chat(payload: ChatRequest, x_agent_key: str = Header(default="")) -> JSONResponse:
        if not app.state.shared_key or x_agent_key != app.state.shared_key:
            return JSONResponse(
                status_code=401, content={"code": "unauthorized", "message": "Invalid agent key"}
            )
        try:
            reply = run_turn(
                store=app.state.store,
                llm=app.state.llm,
                request=payload,
                max_tokens=app.state.max_tokens,
            )
        except SessionExpired:
            # 不是錯誤：Go 會帶完整 evidence 與 messages 重打一次。
            return JSONResponse(
                status_code=409,
                content={"code": "session_expired", "message": "Session not held; resend context"},
            )
        except InvalidTurn as error:
            return JSONResponse(
                status_code=400, content={"code": "invalid_turn", "message": str(error)}
            )
        except LLMError as error:
            return JSONResponse(
                status_code=502, content={"code": "llm_error", "message": str(error)}
            )
        return JSONResponse(status_code=200, content=ChatResponse.from_reply(reply).model_dump())

    return app


def main() -> None:
    import uvicorn

    load_env()
    uvicorn.run(
        create_app(),
        host=os.getenv("AGENT_HOST", "127.0.0.1"),
        port=_int_env("AGENT_PORT", 7795),
    )


if __name__ == "__main__":
    main()
