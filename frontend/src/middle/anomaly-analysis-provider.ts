import type {
  AnomalyAnalysisRequest,
  AnomalyAnalysisResponse,
} from "./anomaly-analysis-contract";

export class AnomalyBackendError extends Error {
  constructor(
    message: string,
    public readonly code:
      | "ANOMALY_BACKEND_NOT_CONFIGURED"
      | "ANOMALY_BACKEND_UNAVAILABLE",
  ) {
    super(message);
  }
}

/**
 * Real anomaly-model boundary. No scoring or explanatory text is generated
 * by the frontend repository.
 */
export async function analyzeGraphWithBackend(
  request: AnomalyAnalysisRequest,
): Promise<AnomalyAnalysisResponse> {
  const backendUrl = process.env.CHAINTRACE_BACKEND_URL?.replace(/\/$/, "");
  if (!backendUrl) {
    throw new AnomalyBackendError(
      "交易資料已取得，等待異常分析後端接口",
      "ANOMALY_BACKEND_NOT_CONFIGURED",
    );
  }

  try {
    const response = await fetch(`${backendUrl}/api/v1/anomaly-analysis`, {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
      },
      body: JSON.stringify(request),
    });
    if (!response.ok) throw new Error(`Backend returned ${response.status}`);
    return (await response.json()) as AnomalyAnalysisResponse;
  } catch (error) {
    if (error instanceof AnomalyBackendError) throw error;
    throw new AnomalyBackendError(
      "交易資料已取得，但異常分析服務目前無法連線",
      "ANOMALY_BACKEND_UNAVAILABLE",
    );
  }
}
