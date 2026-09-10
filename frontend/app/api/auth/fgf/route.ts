import { backendUrl } from "@/src/middle/backend-url";
export const dynamic = "force-dynamic";
export async function GET(request: Request) {
  try {
    const response = await fetch(`${backendUrl()}/Authentication/fgf/login`, {
      redirect: "manual", cache: "no-store", signal: AbortSignal.timeout(12000),
    });
    const location = response.headers.get("location");
    const cookie = response.headers.get("set-cookie");
    if (response.status !== 302 || !location || !cookie) throw new Error("FGF unavailable");
    return new Response(null, { status: 302, headers: {
      Location: location, "Set-Cookie": cookie, "Cache-Control": "no-store", "Referrer-Policy": "no-referrer",
    } });
  } catch {
    return Response.redirect(new URL("/login?error=callback", request.url), 303);
  }
}
