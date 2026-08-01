import type {
  TransactionGraphEdge,
  TransactionGraphNode,
  TransactionGraphResponse,
} from "./transaction-graph-contract";

type BlockscoutAddress = {
  hash?: string;
  name?: string | null;
  ens_domain_name?: string | null;
  is_contract?: boolean;
};

type BlockscoutTransaction = {
  hash?: string;
  from?: BlockscoutAddress;
  to?: BlockscoutAddress | null;
  value?: string;
  timestamp?: string;
};

type BlockscoutResponse = { items?: BlockscoutTransaction[] };

function shortAddress(address: string) {
  return `${address.slice(0, 6)}…${address.slice(-4)}`;
}

function nodeLabel(node: BlockscoutAddress, address: string) {
  return node.name || node.ens_domain_name || shortAddress(address);
}

function graphPosition(index: number, count: number) {
  const angle = (index / Math.max(count, 1)) * Math.PI * 2 - Math.PI / 2;
  return {
    x: 50 + Math.cos(angle) * 34,
    y: 50 + Math.sin(angle) * 34,
  };
}

async function fetchFromChainTraceBackend(
  address: string,
  network: string,
): Promise<TransactionGraphResponse | null> {
  const backendUrl = process.env.CHAINTRACE_BACKEND_URL?.replace(/\/$/, "");
  if (!backendUrl) return null;
  const params = new URLSearchParams({ address, network });
  try {
    const response = await fetch(
      `${backendUrl}/api/v1/transaction-graph?${params}`,
      { headers: { Accept: "application/json" } },
    );
    if (!response.ok) return null;
    return (await response.json()) as TransactionGraphResponse;
  } catch {
    // The teammate backend does not expose this endpoint yet. Keep the
    // frontend usable by falling back to the public Ethereum data source.
    return null;
  }
}

async function fetchEthereumFromBlockscout(
  address: string,
): Promise<TransactionGraphResponse> {
  const response = await fetch(
    `https://eth.blockscout.com/api/v2/addresses/${encodeURIComponent(address)}/transactions`,
    {
      headers: { Accept: "application/json" },
      signal: AbortSignal.timeout(15000),
    },
  );
  if (response.status === 404) {
    throw new Error("找不到這個 Ethereum 地址或尚無交易紀錄");
  }
  if (!response.ok) {
    throw new Error(`鏈上資料服務暫時無法使用（${response.status}）`);
  }

  const payload = (await response.json()) as BlockscoutResponse;
  const transactions = (payload.items || []).slice(0, 30);
  const normalizedTarget = address.toLowerCase();
  const peers = new Map<string, BlockscoutAddress>();

  for (const transaction of transactions) {
    for (const endpoint of [transaction.from, transaction.to]) {
      const hash = endpoint?.hash;
      if (hash && hash.toLowerCase() !== normalizedTarget) {
        peers.set(hash.toLowerCase(), endpoint);
      }
    }
  }

  const peerEntries = [...peers.entries()].slice(0, 18);
  const nodes: TransactionGraphNode[] = [
    {
      id: normalizedTarget,
      address,
      label: shortAddress(address),
      type: "focus",
      x: 50,
      y: 50,
    },
    ...peerEntries.map(([id, node], index) => ({
      id,
      address: node.hash || id,
      label: nodeLabel(node, node.hash || id),
      type: node.is_contract ? ("contract" as const) : ("normal" as const),
      ...graphPosition(index, peerEntries.length),
    })),
  ];
  const visibleIds = new Set(nodes.map((node) => node.id));
  const edges: TransactionGraphEdge[] = transactions
    .map((transaction, index) => {
      const from = transaction.from?.hash?.toLowerCase();
      const to = transaction.to?.hash?.toLowerCase();
      if (!from || !to || !visibleIds.has(from) || !visibleIds.has(to)) {
        return null;
      }
      return {
        id: transaction.hash || `${from}-${to}-${index}`,
        from,
        to,
        value: Number(transaction.value || 0) / 1e18,
        asset: "ETH",
        timestamp: transaction.timestamp || null,
      };
    })
    .filter((edge): edge is TransactionGraphEdge => edge !== null);

  return {
    address,
    network: "Ethereum",
    nodes,
    edges,
    transactionCount: transactions.length,
    totalFlow: edges.reduce((sum, edge) => sum + edge.value, 0),
    flowAsset: "ETH",
    source: "blockscout-ethereum-mainnet",
    updatedAt: new Date().toISOString(),
  };
}

export async function fetchTransactionGraph(
  address: string,
  network: string,
) {
  const backendResult = await fetchFromChainTraceBackend(address, network);
  if (backendResult) return backendResult;
  if (network.toLowerCase() !== "ethereum") {
    throw new Error("目前實際鏈上圖譜先支援 Ethereum");
  }
  return fetchEthereumFromBlockscout(address);
}
