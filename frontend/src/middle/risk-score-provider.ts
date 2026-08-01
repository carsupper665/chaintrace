import {
  DEFAULT_RISK_SCORE,
  type RiskScoreRequest,
  type RiskScoreResponse,
} from "./risk-score-contract";

/**
 * Backend integration boundary.
 *
 * The scoring implementation is intentionally not included. The backend owner
 * only needs to replace this function and keep returning RiskScoreResponse.
 */
export async function fetchRiskScoreFromBackend(
  request: RiskScoreRequest,
): Promise<RiskScoreResponse> {
  return {
    ...request,
    score: DEFAULT_RISK_SCORE,
    status: "pending",
    source: "default",
    updatedAt: null,
  };
}
