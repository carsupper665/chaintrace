import { proxyInvestigationStream } from "@/src/middle/investigation-backend";
import { readCredential } from "@/src/auth/session";

export const dynamic = "force-dynamic";

const responseHeaders = { "Cache-Control": "no-store" };
type RouteContext = { params: Promise<{ id: string }> };

async function backendPath(context: RouteContext) {
  const { id } = await context.params;
  return `/api/v1/investigations/${encodeURIComponent(id)}/conversation/stream`;
}

export async function POST(request: Request, context: RouteContext) {
  const path = await backendPath(context);
  if (!readCredential(request)) {
    return proxyInvestigationStream(request, path);
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
    model?: unknown;
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

  return proxyInvestigationStream(
    request,
    path,
    JSON.stringify({
      idempotencyKey: command.idempotencyKey,
      message: command.message,
      ...(typeof command.model === "string" && command.model
        ? { model: command.model }
        : {}),
    }),
  );
}
