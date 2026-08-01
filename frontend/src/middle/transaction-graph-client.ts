import {
  TRANSACTION_GRAPH_API_PATH,
  type TransactionGraphResponse,
} from "./transaction-graph-contract";

export class TransactionGraphClientError extends Error {
  constructor(
    message: string,
    public readonly status: number,
  ) {
    super(message);
  }
}

export async function getTransactionGraph(
  address: string,
  network: string,
): Promise<TransactionGraphResponse> {
  const params = new URLSearchParams({ address, network });
  const response = await fetch(`${TRANSACTION_GRAPH_API_PATH}?${params}`, {
    headers: { Accept: "application/json" },
  });
  const result = (await response.json()) as
    | TransactionGraphResponse
    | { error?: string };

  if (!response.ok) {
    throw new TransactionGraphClientError(
      "error" in result && result.error
        ? result.error
        : "無法取得鏈上交易資料",
      response.status,
    );
  }
  return result as TransactionGraphResponse;
}
