import { readCredential } from "@/src/auth/session";
import { backendUrl } from "@/src/middle/backend-url";

const responseHeaders = { "Cache-Control": "no-store" };

export async function proxyInvestigationRequest(
  request: Request,
  backendPath: string,
  bodyOverride?: string,
) {
  const token = readCredential(request);
  if (!token) {
    return Response.json(
      {
        code: "unauthorized",
        error: "Authentication required",
      },
      { status: 401, headers: responseHeaders },
    );
  }

  try {
    const method = request.method.toUpperCase();
    const body =
      method === "POST" || method === "PATCH"
        ? bodyOverride ?? (await request.text())
        : undefined;
    const response = await fetch(`${backendUrl()}${backendPath}`, {
      method,
      headers: {
        Accept: "application/json",
        Authorization: `Bearer ${token}`,
        ...(body ? { "Content-Type": "application/json" } : {}),
      },
      body,
      cache: "no-store",
    });
    const contentType = response.headers.get("content-type");
    const headers = new Headers(responseHeaders);
    if (contentType) headers.set("Content-Type", contentType);
    return new Response(response.status === 204 ? null : await response.text(), {
      status: response.status,
      headers,
    });
  } catch {
    return Response.json(
      {
        code: "investigation_service_unavailable",
        error: "Investigation service unavailable",
      },
      { status: 503, headers: responseHeaders },
    );
  }
}

// proxyInvestigationRequest's streaming twin: pipes the backend's response
// body straight through instead of buffering it with `.text()`, so an SSE
// reply forwards its frames as they arrive rather than only after the whole
// turn is done. The Workers runtime this app runs on (vinext/workerd) natively
// supports streaming fetch bodies in both directions, so this is otherwise
// identical to proxyInvestigationRequest.
export async function proxyInvestigationStream(
  request: Request,
  backendPath: string,
  bodyOverride?: string,
) {
  const token = readCredential(request);
  if (!token) {
    return Response.json(
      {
        code: "unauthorized",
        error: "Authentication required",
      },
      { status: 401, headers: responseHeaders },
    );
  }

  try {
    const body = bodyOverride ?? (await request.text());
    const response = await fetch(`${backendUrl()}${backendPath}`, {
      method: "POST",
      headers: {
        Accept: "text/event-stream",
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body,
      cache: "no-store",
    });
    if (!response.ok || !response.body) {
      // A rejection before any streaming started (bad key, bad JSON,
      // agent_unavailable) is still a normal, fully-buffered JSON body.
      const contentType = response.headers.get("content-type");
      const headers = new Headers(responseHeaders);
      if (contentType) headers.set("Content-Type", contentType);
      return new Response(await response.text(), { status: response.status, headers });
    }
    const headers = new Headers(responseHeaders);
    headers.set("Content-Type", "text/event-stream");
    return new Response(response.body, { status: response.status, headers });
  } catch {
    return Response.json(
      {
        code: "investigation_service_unavailable",
        error: "Investigation service unavailable",
      },
      { status: 503, headers: responseHeaders },
    );
  }
}
