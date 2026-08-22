import { exchangeLoginChallenge } from "@/src/auth/backend";
import { credentialCookie } from "@/src/auth/session";

export const dynamic = "force-dynamic";

function redirect(request: Request, path: string, cookie?: string) {
  const headers = new Headers({
    "Cache-Control": "no-store",
    Location: new URL(path, request.url).toString(),
  });
  if (cookie) headers.set("Set-Cookie", cookie);
  return new Response(null, { status: 303, headers });
}

export async function GET(request: Request) {
  const url = new URL(request.url);
  const code = url.searchParams.get("code")?.trim();
  const id = url.searchParams.get("id")?.trim();
  if (!code || !id) return redirect(request, "/login?error=callback");

  try {
    const token = await exchangeLoginChallenge(code, id);
    if (!token) return redirect(request, "/login?error=callback");
    return redirect(request, "/", credentialCookie(token));
  } catch {
    return redirect(request, "/login?error=callback");
  }
}
