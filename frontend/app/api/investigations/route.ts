import { proxyInvestigationRequest } from "@/src/middle/investigation-backend";

export const dynamic = "force-dynamic";

export function GET(request: Request) {
  const search = new URL(request.url).search;
  return proxyInvestigationRequest(
    request,
    `/api/v1/investigations${search}`,
  );
}

export function POST(request: Request) {
  return proxyInvestigationRequest(request, "/api/v1/investigations");
}
