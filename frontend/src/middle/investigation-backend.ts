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
