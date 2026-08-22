import { proxyInvestigationRequest } from "@/src/middle/investigation-backend";
import { readCredential } from "@/src/auth/session";

export const dynamic = "force-dynamic";

const responseHeaders = { "Cache-Control": "no-store" };
type RouteContext = { params: Promise<{ id: string }> };

async function backendPath(context: RouteContext, request?: Request) {
  const { id } = await context.params;
  const query = new URLSearchParams();
  if (request) {
    const incoming = new URL(request.url).searchParams;
    for (const name of ["cursor", "pageSize"]) {
      const value = incoming.get(name);
      if (value !== null) query.set(name, value);
    }
  }
  const search = query.size > 0 ? `?${query}` : "";
  return `/api/v1/investigations/${encodeURIComponent(id)}/conversation${search}`;
}

export async function GET(request: Request, context: RouteContext) {
  return proxyInvestigationRequest(request, await backendPath(context, request));
}

export async function POST(request: Request, context: RouteContext) {
  const path = await backendPath(context);
  if (!readCredential(request)) {
    return proxyInvestigationRequest(request, path);
  }
  let payload: unknown;
  try {
    payload = await request.json();
  } catch {
    payload = null;
  }
  const command = (payload || {}) as {
    idempotencyKey?: unknown;
    message?: unknown;
  };
  if (
    typeof command.idempotencyKey !== "string" ||
    !command.idempotencyKey.trim() ||
    typeof command.message !== "string" ||
    !command.message.trim()
  ) {
    return Response.json(
      {
        code: "invalid_conversation_request",
        error: "idempotencyKey and message are required",
      },
      { status: 400, headers: responseHeaders },
    );
  }

  return proxyInvestigationRequest(
    request,
    path,
    JSON.stringify({
      idempotencyKey: command.idempotencyKey,
      message: command.message,
    }),
  );
}
