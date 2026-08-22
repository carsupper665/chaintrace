import { fetchOwnerIdentity } from "@/src/auth/backend";
import { readCredential } from "@/src/auth/session";

export const dynamic = "force-dynamic";

const responseHeaders = { "Cache-Control": "no-store" };

export async function GET(request: Request) {
  const token = readCredential(request);
  if (!token) {
    return Response.json(
      { error: "Authentication required" },
      { status: 401, headers: responseHeaders },
    );
  }

  try {
    const result = await fetchOwnerIdentity(token);
    if (!result.owner) {
      return Response.json(
        { error: "Authentication required" },
        { status: result.status, headers: responseHeaders },
      );
    }
    return Response.json(result.owner, { headers: responseHeaders });
  } catch {
    return Response.json(
      { error: "Identity service unavailable" },
      { status: 503, headers: responseHeaders },
    );
  }
}
