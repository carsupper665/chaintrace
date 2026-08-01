import {
  DEFAULT_INVESTIGATION_METRICS,
  INVESTIGATION_METRICS_API_PATH,
  normalizeNonNegativeNumber,
  type InvestigationMetricsRequest,
  type InvestigationMetricsResponse,
} from "./investigation-metrics-contract";

export async function getInvestigationMetrics(
  request: InvestigationMetricsRequest,
): Promise<InvestigationMetricsResponse> {
  const params = new URLSearchParams(request);

  try {
    const response = await fetch(`${INVESTIGATION_METRICS_API_PATH}?${params}`, {
      headers: { Accept: "application/json" },
    });

    if (!response.ok) throw new Error(`Metrics request failed: ${response.status}`);

    const result = (await response.json()) as InvestigationMetricsResponse;
    return {
      ...result,
      relatedNodes: Math.round(normalizeNonNegativeNumber(result.relatedNodes)),
      totalFlow: normalizeNonNegativeNumber(result.totalFlow),
      transactionCount: Math.round(
        normalizeNonNegativeNumber(result.transactionCount),
      ),
    };
  } catch {
    return {
      ...request,
      ...DEFAULT_INVESTIGATION_METRICS,
      status: "unavailable",
      source: "frontend-fallback",
      updatedAt: null,
    };
  }
}
