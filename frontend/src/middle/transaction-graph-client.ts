import {
  transactionGraphApiPath,
  type SavedTransactionGraphEdge,
  type SavedTransactionGraphNode,
  type TransactionGraphPageRequest,
  type TransactionGraphPageResponse,
} from "./transaction-graph-contract.ts";
import type { InvestigationErrorResponse } from "./investigation-contract.ts";
import { expireSession } from "@/src/auth/expire";

type Fetch = typeof fetch;

export class TransactionGraphClientError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(message: string, status: number, code: string) {
    super(message);
    this.name = "TransactionGraphClientError";
    this.status = status;
    this.code = code;
  }
}

function stableErrorMessage(
  status: number,
  code: string,
  response: InvestigationErrorResponse,
) {
  if (status === 401 || code === "unauthorized") {
    expireSession();
    return "登入狀態已失效，請重新登入。";
  }
  if (status === 404 || code === "investigation_not_found") {
    return "找不到這筆調查，可能已被刪除或無權存取。";
  }
  if (code === "stale_dataset") {
    return "分析資料集已更新，請重新載入目前結果。";
  }
  if (status === 429 || code === "rate_limited") {
    return "短時間內的請求過多，請稍候再試。";
  }
  return response.error || response.message || "無法載入交易圖譜，請稍後再試。";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function isNode(value: unknown): value is SavedTransactionGraphNode {
  if (!isRecord(value)) return false;
  return (
    typeof value.id === "string" &&
    value.id.length > 0 &&
    typeof value.address === "string" &&
    value.address.length > 0 &&
    (value.type === "focus" || value.type === "normal" || value.type === "contract") &&
    !("x" in value) &&
    !("y" in value) &&
    !("group" in value)
  );
}

function isEdge(value: unknown): value is SavedTransactionGraphEdge {
  if (!isRecord(value) || !isRecord(value.amount)) return false;
  return (
    typeof value.id === "string" &&
    value.id.length > 0 &&
    typeof value.transactionHash === "string" &&
    value.transactionHash.length > 0 &&
    typeof value.eventIdentity === "string" &&
    value.eventIdentity.length > 0 &&
    typeof value.from === "string" &&
    value.from.length > 0 &&
    typeof value.to === "string" &&
    value.to.length > 0 &&
    typeof value.amount.smallestUnit === "string" &&
    /^\d+$/.test(value.amount.smallestUnit) &&
    Number.isInteger(value.amount.decimals) &&
    Number(value.amount.decimals) >= 0 &&
    typeof value.amount.asset === "string" &&
    value.amount.asset.length > 0 &&
    typeof value.timestamp === "string" &&
    value.timestamp.length > 0 &&
    !("group" in value)
  );
}

function hasUniqueIds(values: Array<{ id: string }>) {
  return new Set(values.map((value) => value.id)).size === values.length;
}

function parseGraphPage(value: unknown): TransactionGraphPageResponse | null {
  if (!isRecord(value) || !Array.isArray(value.nodes) || !Array.isArray(value.edges)) {
    return null;
  }
  if (!value.nodes.every(isNode) || !value.edges.every(isEdge)) return null;
  if (!hasUniqueIds(value.nodes) || !hasUniqueIds(value.edges)) return null;
  if (
    typeof value.datasetId !== "string" ||
    value.datasetId.length === 0 ||
    typeof value.hasMore !== "boolean" ||
    !(value.nextCursor === null || typeof value.nextCursor === "string") ||
    (value.hasMore && !value.nextCursor) ||
    (!value.hasMore && value.nextCursor !== null)
  ) {
    return null;
  }
  return value as TransactionGraphPageResponse;
}

export async function getTransactionGraphPage(
  investigationId: string,
  request: TransactionGraphPageRequest,
  fetchImpl: Fetch = fetch,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ datasetId: request.datasetId });
  if (request.cursor) query.set("cursor", request.cursor);
  if (request.pageSize !== undefined) query.set("pageSize", String(request.pageSize));
  if (request.anchor) query.set("anchor", request.anchor);

  const response = await fetchImpl(
    `${transactionGraphApiPath(investigationId)}?${query}`,
    {
      headers: { Accept: "application/json" },
      cache: "no-store",
      signal,
    },
  );
  let payload: unknown = null;
  try {
    payload = await response.json();
  } catch {
    // Invalid JSON is normalized below to one stable client error.
  }

  if (!response.ok) {
    const error = isRecord(payload) ? (payload as InvestigationErrorResponse) : {};
    const code = error.code || `HTTP_${response.status}`;
    throw new TransactionGraphClientError(
      stableErrorMessage(response.status, code, error),
      response.status,
      code,
    );
  }

  const page = parseGraphPage(payload);
  if (!page) {
    throw new TransactionGraphClientError(
      "交易圖譜服務回傳了無法辨識的資料。",
      502,
      "invalid_graph_response",
    );
  }
  return page;
}
