import { proxyInvestigationRequest } from "@/src/middle/investigation-backend";

export const dynamic = "force-dynamic";

type RouteContext = { params: Promise<{ id: string }> };

export async function GET(request: Request, context: RouteContext) {
  const { id } = await context.params;
  const incoming = new URL(request.url).searchParams;
  const query = new URLSearchParams();
  for (const name of ["datasetId", "cursor", "pageSize", "anchor"]) {
    const value = incoming.get(name);
    if (value !== null) query.set(name, value);
  }
  const search = query.size > 0 ? `?${query}` : "";

  return proxyInvestigationRequest(
    request,
    `/api/v1/investigations/${encodeURIComponent(id)}/graph${search}`,
  );
}
