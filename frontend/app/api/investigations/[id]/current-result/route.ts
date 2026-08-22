import { proxyInvestigationRequest } from "@/src/middle/investigation-backend";

export const dynamic = "force-dynamic";

type RouteContext = { params: Promise<{ id: string }> };

export async function GET(request: Request, context: RouteContext) {
  const { id } = await context.params;
  return proxyInvestigationRequest(
    request,
    `/api/v1/investigations/${encodeURIComponent(id)}/current-result`,
  );
}
