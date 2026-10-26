import { proxyInvestigationRequest } from "@/src/middle/investigation-backend";

export const dynamic = "force-dynamic";

export function POST(request: Request) {
  return proxyInvestigationRequest(request, "/api/v1/claims/verify-address");
}
