import { proxyClaimUpload } from "@/src/middle/investigation-backend";

export const dynamic = "force-dynamic";

export function POST(request: Request) {
  return proxyClaimUpload(request, "/api/v1/claims/extract");
}
