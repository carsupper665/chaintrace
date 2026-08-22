import type { ExactAmount } from "./investigation-contract.ts";

export const TRANSACTION_GRAPH_PAGE_SIZE = 100;

export type TransactionGraphNodeType = "focus" | "normal" | "contract";

export type SavedTransactionGraphNode = {
  id: string;
  address: string;
  type: TransactionGraphNodeType;
};

export type SavedTransactionGraphEdge = {
  id: string;
  transactionHash: string;
  eventIdentity: string;
  from: string;
  to: string;
  amount: ExactAmount;
  timestamp: string;
};

export type TransactionGraphPageResponse = {
  datasetId: string;
  nodes: SavedTransactionGraphNode[];
  edges: SavedTransactionGraphEdge[];
  nextCursor: string | null;
  hasMore: boolean;
};

export type TransactionGraphPageRequest = {
  datasetId: string;
  cursor?: string;
  pageSize?: number;
  anchor?: string;
};

export type TransactionGraphNode = SavedTransactionGraphNode & {
  label: string;
  x: number;
  y: number;
  group: number;
};

export type TransactionGraphEdge = SavedTransactionGraphEdge & {
  group: number;
};

// Browser rendering model. Layout and aggregate result fields never come from Graph API.
export type TransactionGraphResponse = {
  datasetId: string;
  address: string;
  network: string;
  nodes: TransactionGraphNode[];
  edges: TransactionGraphEdge[];
  nextCursor: string | null;
  hasMore: boolean;
  transactionCount: number;
  totalFlow: ExactAmount;
  flowAsset: string;
  source: string;
  updatedAt: string;
};

export const transactionGraphApiPath = (investigationId: string) =>
  `/api/investigations/${encodeURIComponent(investigationId)}/graph`;
