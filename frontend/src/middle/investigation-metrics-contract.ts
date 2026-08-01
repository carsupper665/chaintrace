export type InvestigationMetricsStatus = "pending" | "ready" | "unavailable";

export type InvestigationMetricsRequest = {
  address: string;
  network: string;
};

export type InvestigationMetricsResponse = InvestigationMetricsRequest & {
  relatedNodes: number;
  totalFlow: number;
  flowAsset: string;
  transactionCount: number;
  status: InvestigationMetricsStatus;
  source: string;
  updatedAt: string | null;
};

export const DEFAULT_INVESTIGATION_METRICS = {
  relatedNodes: 0,
  totalFlow: 0,
  flowAsset: "",
  transactionCount: 0,
} as const;

export const INVESTIGATION_METRICS_API_PATH =
  "/api/middle/investigation-metrics";

export function normalizeNonNegativeNumber(value: unknown): number {
  const result = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(result)) return 0;
  return Math.max(0, result);
}
