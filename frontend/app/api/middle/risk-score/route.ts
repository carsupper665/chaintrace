import { fetchRiskScoreFromBackend } from "@/src/middle/risk-score-provider";

export const dynamic = "force-dynamic";

export async function GET(request: Request) {
  const url = new URL(request.url);
  const address = url.searchParams.get("address")?.trim() ?? "";
  const network = url.searchParams.get("network")?.trim() ?? "";

  if (!address || !network) {
    return Response.json(
      { error: "address and network are required" },
      { status: 400 },
    );
  }

  const result = await fetchRiskScoreFromBackend({ address, network });

  return Response.json(result, {
    headers: { "Cache-Control": "no-store" },
  });
}
