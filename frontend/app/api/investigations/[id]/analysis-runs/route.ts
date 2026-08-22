import { proxyInvestigationRequest } from "@/src/middle/investigation-backend";

export const dynamic = "force-dynamic";

type RouteContext = { params: Promise<{ id: string }> };

export async function POST(request: Request, context: RouteContext) {
  const { id } = await context.params;
  return proxyInvestigationRequest(
    request,
    `/api/v1/investigations/${encodeURIComponent(id)}/analysis-runs`,
  );
}
