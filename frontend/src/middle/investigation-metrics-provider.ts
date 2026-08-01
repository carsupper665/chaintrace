import {
  DEFAULT_INVESTIGATION_METRICS,
  type InvestigationMetricsRequest,
  type InvestigationMetricsResponse,
} from "./investigation-metrics-contract";

/**
 * Backend integration boundary. Graph analysis and aggregation are
 * intentionally not implemented in this frontend repository.
 */
export async function fetchInvestigationMetricsFromBackend(
  request: InvestigationMetricsRequest,
): Promise<InvestigationMetricsResponse> {
  return {
    ...request,
    ...DEFAULT_INVESTIGATION_METRICS,
    status: "pending",
    source: "default",
    updatedAt: null,
  };
}
