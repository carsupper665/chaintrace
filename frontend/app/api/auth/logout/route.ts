import { revokeOwnerCredential } from "@/src/auth/backend";
import {
  clearedCredentialCookie,
  readCredential,
} from "@/src/auth/session";

export const dynamic = "force-dynamic";

export async function POST(request: Request) {
  const token = readCredential(request);
  if (token) {
    try {
      await revokeOwnerCredential(token);
    } catch {
      // Local logout still removes browser access when the backend is unavailable.
    }
  }

  return new Response(null, {
    status: 303,
    headers: {
      "Cache-Control": "no-store",
      Location: new URL("/login", request.url).toString(),
      "Set-Cookie": clearedCredentialCookie(),
    },
  });
}
