import { proxyInvestigationRequest } from "@/src/middle/investigation-backend";

export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ id: string; runId: string }>;
};

export async function GET(request: Request, context: RouteContext) {
  const { id, runId } = await context.params;
  return proxyInvestigationRequest(
    request,
    `/api/v1/investigations/${encodeURIComponent(id)}/analysis-runs/${encodeURIComponent(runId)}`,
  );
}

export async function DELETE(request: Request, context: RouteContext) {
  const { id, runId } = await context.params;
  return proxyInvestigationRequest(
    request,
    `/api/v1/investigations/${encodeURIComponent(id)}/analysis-runs/${encodeURIComponent(runId)}`,
  );
}
