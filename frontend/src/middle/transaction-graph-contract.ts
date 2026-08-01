export type TransactionGraphNode = {
  id: string;
  address: string;
  label: string;
  type: "focus" | "normal" | "contract";
  x: number;
  y: number;
  group?: number;
};

export type TransactionGraphEdge = {
  id: string;
  from: string;
  to: string;
  value: number;
  asset: string;
  timestamp: string | null;
  group?: number;
};

export type TransactionGraphResponse = {
  address: string;
  network: string;
  nodes: TransactionGraphNode[];
  edges: TransactionGraphEdge[];
  transactionCount: number;
  totalFlow: number;
  flowAsset: string;
  source: string;
  updatedAt: string;
};

export const TRANSACTION_GRAPH_API_PATH = "/api/middle/transaction-graph";
