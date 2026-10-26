"""POST /v1/extract-claims —— 給 Go 呼叫的抽取入口。"""

from fastapi import APIRouter, Header, Request
from fastapi.responses import JSONResponse

from .schemas import ExtractClaimsRequest
from .service import ExtractError, extract_claims

router = APIRouter()


@router.post("/v1/extract-claims")
def extract(
    payload: ExtractClaimsRequest, request: Request, x_agent_key: str = Header(default="")
) -> JSONResponse:
    shared_key = getattr(request.app.state, "shared_key", "")
    if not shared_key or x_agent_key != shared_key:
        return JSONResponse(
            status_code=401, content={"code": "unauthorized", "message": "Invalid agent key"}
        )
    try:
        result = extract_claims(request.app.state.llm, payload)
    except ExtractError as error:
        status = 400
        if error.code == "llm_error":
            status = 502
        elif error.code == "internal_error":
            status = 500
        return JSONResponse(
            status_code=status, content={"code": error.code, "message": error.message}
        )
    return JSONResponse(status_code=200, content=result.model_dump())
