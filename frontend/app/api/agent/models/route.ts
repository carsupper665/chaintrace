import { proxyInvestigationRequest } from "@/src/middle/investigation-backend";

export const dynamic = "force-dynamic";

// The compatible models the Agent can run. Only ids and display names come
// back; endpoint URLs and tokens never leave the Python sidecar.
export async function GET(request: Request) {
  return proxyInvestigationRequest(request, "/api/v1/agent/models");
}
