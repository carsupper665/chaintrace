import {
  ANOMALY_ANALYSIS_API_PATH,
  type AnomalyAnalysisRequest,
  type AnomalyAnalysisResponse,
} from "./anomaly-analysis-contract";

export class AnomalyAnalysisClientError extends Error {
  constructor(
    message: string,
    public readonly code: string,
  ) {
    super(message);
  }
}

export async function requestAnomalyAnalysis(
  request: AnomalyAnalysisRequest,
): Promise<AnomalyAnalysisResponse> {
  const response = await fetch(ANOMALY_ANALYSIS_API_PATH, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(request),
  });
  const result = (await response.json()) as
    | AnomalyAnalysisResponse
    | { error?: string; code?: string };
  if (!response.ok) {
    throw new AnomalyAnalysisClientError(
      "error" in result && result.error
        ? result.error
        : "異常分析服務無法使用",
      ("code" in result && result.code) || "ANALYSIS_UNAVAILABLE",
    );
  }
  return result as AnomalyAnalysisResponse;
}
