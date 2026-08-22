import { requestLogin } from "@/src/auth/backend";

export const dynamic = "force-dynamic";

const responseHeaders = { "Cache-Control": "no-store" };

export async function POST(request: Request) {
  let payload: { identifier?: unknown; password?: unknown };
  try {
    payload = (await request.json()) as typeof payload;
  } catch {
    return Response.json(
      { error: "請輸入帳號與密碼" },
      { status: 400, headers: responseHeaders },
    );
  }

  const identifier =
    typeof payload.identifier === "string" ? payload.identifier.trim() : "";
  const password = typeof payload.password === "string" ? payload.password : "";
  if (!identifier || !password) {
    return Response.json(
      { error: "請輸入帳號與密碼" },
      { status: 400, headers: responseHeaders },
    );
  }

  try {
    const backendResponse = await requestLogin(identifier, password);
    if (backendResponse.status === 202) {
      return Response.json(
        { status: "waiting" },
        { status: 202, headers: responseHeaders },
      );
    }

    if (backendResponse.status === 429) {
      return Response.json(
        { error: "登入嘗試次數過多，請稍後再試" },
        { status: 429, headers: responseHeaders },
      );
    }
    if (backendResponse.status >= 500) {
      return Response.json(
        { error: "登入服務暫時無法使用，請稍後再試" },
        { status: 503, headers: responseHeaders },
      );
    }
    return Response.json(
      { error: "登入資料無效，請確認後再試一次" },
      { status: 401, headers: responseHeaders },
    );
  } catch {
    return Response.json(
      { error: "登入服務暫時無法使用，請稍後再試" },
      { status: 503, headers: responseHeaders },
    );
  }
}
