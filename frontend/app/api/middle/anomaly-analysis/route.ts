import { analyzeGraphWithBackend } from "@/src/middle/anomaly-analysis-provider";
import type { AnomalyAnalysisRequest } from "@/src/middle/anomaly-analysis-contract";

export const dynamic = "force-dynamic";

export async function POST(request: Request) {
  let body: AnomalyAnalysisRequest;
  try {
    body = (await request.json()) as AnomalyAnalysisRequest;
  } catch {
    return Response.json({ error: "請求格式不正確" }, { status: 400 });
  }
  if (!body.address || !body.graph?.nodes?.length) {
    return Response.json(
      { error: "必須先提供已取得的交易圖譜資料" },
      { status: 400 },
    );
  }
  try {
    return Response.json(await analyzeGraphWithBackend(body), {
      headers: { "Cache-Control": "no-store" },
    });
  } catch (error) {
    const result =
      error instanceof Error
        ? {
            error: error.message,
            code:
              "code" in error && typeof error.code === "string"
                ? error.code
                : "ANALYSIS_UNAVAILABLE",
          }
        : { error: "異常分析服務無法使用", code: "ANALYSIS_UNAVAILABLE" };
    return Response.json(result, { status: 503 });
  }
}
