export type RiskScoreStatus = "pending" | "ready" | "unavailable";

export type RiskScoreRequest = {
  address: string;
  network: string;
};

export type RiskScoreResponse = RiskScoreRequest & {
  score: number;
  status: RiskScoreStatus;
  source: string;
  updatedAt: string | null;
};

export const DEFAULT_RISK_SCORE = 0;
export const RISK_SCORE_API_PATH = "/api/middle/risk-score";

export function normalizeRiskScore(value: unknown): number {
  const score = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(score)) return DEFAULT_RISK_SCORE;
  return Math.min(100, Math.max(0, Math.round(score)));
}
