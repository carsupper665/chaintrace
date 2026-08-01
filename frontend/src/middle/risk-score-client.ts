import {
  DEFAULT_RISK_SCORE,
  RISK_SCORE_API_PATH,
  normalizeRiskScore,
  type RiskScoreRequest,
  type RiskScoreResponse,
} from "./risk-score-contract";

export async function getRiskScore(
  request: RiskScoreRequest,
): Promise<RiskScoreResponse> {
  const params = new URLSearchParams(request);

  try {
    const response = await fetch(`${RISK_SCORE_API_PATH}?${params}`, {
      headers: { Accept: "application/json" },
    });

    if (!response.ok) throw new Error(`Risk score request failed: ${response.status}`);

    const result = (await response.json()) as RiskScoreResponse;
    return { ...result, score: normalizeRiskScore(result.score) };
  } catch {
    return {
      ...request,
      score: DEFAULT_RISK_SCORE,
      status: "unavailable",
      source: "frontend-fallback",
      updatedAt: null,
    };
  }
}
