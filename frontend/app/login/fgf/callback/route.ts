import { backendUrl } from "@/src/middle/backend-url";
import { credentialCookie } from "@/src/auth/session";
export const dynamic = "force-dynamic";
export async function GET(request: Request) {
  const url = new URL(request.url);
  const headers = new Headers({ "Cache-Control": "no-store", "Referrer-Policy": "no-referrer" });
  headers.set("Location", new URL("/login?error=callback", url).toString());
  headers.append("Set-Cookie", `chaintrace_fgf_flow=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0${url.protocol === "https:" ? "; Secure" : ""}`);
  try {
    if (url.searchParams.has("error")) throw new Error("FGF denied");
    const response = await fetch(`${backendUrl()}/Authentication/fgf/callback`, {
      method: "POST", cache: "no-store", signal: AbortSignal.timeout(12000),
      headers: { "Content-Type": "application/json", Cookie: request.headers.get("cookie") || "" },
      body: JSON.stringify({ code: url.searchParams.get("code"), state: url.searchParams.get("state") }),
    });
    if (!response.ok) throw new Error(`FGF failed: API answered ${response.status} ${await response.text()}`);
    const payload = await response.json() as { token?: unknown };
    if (typeof payload.token !== "string" || !payload.token) throw new Error("Missing credential");
    headers.append("Set-Cookie", credentialCookie(payload.token));
    headers.set("Location", new URL("/", url).toString());
  } catch (error) {
    console.error("FGF callback:", error instanceof Error ? error.message : error);
  }
  return new Response(null, { status: 303, headers });
}
