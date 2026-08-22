import { proxyInvestigationRequest } from "@/src/middle/investigation-backend";

export const dynamic = "force-dynamic";

type RouteContext = { params: Promise<{ id: string }> };

async function proxyDetail(request: Request, context: RouteContext) {
  const { id } = await context.params;
  return proxyInvestigationRequest(
    request,
    `/api/v1/investigations/${encodeURIComponent(id)}`,
  );
}

export function GET(request: Request, context: RouteContext) {
  return proxyDetail(request, context);
}

export function PATCH(request: Request, context: RouteContext) {
  return proxyDetail(request, context);
}

export function DELETE(request: Request, context: RouteContext) {
  return proxyDetail(request, context);
}
