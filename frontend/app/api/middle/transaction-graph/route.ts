import { fetchTransactionGraph } from "@/src/middle/transaction-graph-provider";

export const dynamic = "force-dynamic";

export async function GET(request: Request) {
  const url = new URL(request.url);
  const address = url.searchParams.get("address")?.trim() || "";
  const network = url.searchParams.get("network")?.trim() || "Ethereum";

  if (!/^0x[a-fA-F0-9]{40}$/.test(address)) {
    return Response.json(
      { error: "請輸入有效的 Ethereum 錢包地址（0x 加 40 個十六進位字元）" },
      { status: 400 },
    );
  }

  try {
    const graph = await fetchTransactionGraph(address, network);
    return Response.json(graph, {
      headers: { "Cache-Control": "no-store" },
    });
  } catch (error) {
    return Response.json(
      {
        error:
          error instanceof Error ? error.message : "無法取得實際鏈上交易資料",
      },
      { status: 502 },
    );
  }
}
