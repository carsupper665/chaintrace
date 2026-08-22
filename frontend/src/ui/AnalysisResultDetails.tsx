import type { CurrentAnalysisResult } from "@/src/middle/investigation-contract";
import { formatExactAmount } from "@/src/middle/investigation-client";

export function AnalysisResultDetails({
  result,
  attemptError = "",
}: {
  result: CurrentAnalysisResult;
  attemptError?: string;
}) {
  const { assessment, dataset, metrics } = result;
  const hasScore = assessment.score !== null;

  return (
    <div className="analysis-result-details" aria-label="目前分析結果">
      {attemptError && (
        <div className="stable-result-warning" role="alert">
          <strong>新一輪分析未完成，既有穩定結果仍保留。</strong>
          <span>{attemptError}</span>
        </div>
      )}
      {dataset.partial && (
        <div className="partial-analysis-warning" role="alert">
          <strong>部分分析</strong>
          <span>
            證據收集中途停止，此分數僅適用於已收集資料，請連同 coverage 與信心度判讀。
          </span>
        </div>
      )}

      <div className="result-facts">
        <div>
          <small>風險評估</small>
          {hasScore ? (
            <strong>
              {assessment.score}/100 · {assessment.level}
            </strong>
          ) : (
            <strong className="insufficient-evidence">insufficient evidence</strong>
          )}
        </div>
        <div>
          <small>總資金流</small>
          <strong>
            {formatExactAmount(metrics.totalFlow)} {metrics.totalFlow.asset}
          </strong>
        </div>
        <div>
          <small>Dataset metrics</small>
          <strong>
            {metrics.transferCount} 筆 Transfer · {metrics.relatedNodes} 個關聯地址
          </strong>
        </div>
        <div>
          <small>評估來源</small>
          <strong>{assessment.source}</strong>
        </div>
      </div>

      <div className="coverage-grid" aria-label="Analysis coverage">
        <div>
          <small>Requested scope</small>
          <strong>
            Transfer 上限 {dataset.transferLimit} · 深度 {dataset.traversalDepth}
          </strong>
        </div>
        <div>
          <small>Collected coverage</small>
          <strong>
            已收集 {dataset.collectedTransfers} / {dataset.transferLimit} · 已到達{" "}
            {dataset.reachedDepth} / {dataset.traversalDepth}
          </strong>
        </div>
        <div>
          <small>固定 30 天 window</small>
          <strong>
            {dataset.windowStart.slice(0, 10)} → {dataset.windowEnd.slice(0, 10)}
          </strong>
        </div>
        <div>
          <small>Collection outcome</small>
          <strong>
            {dataset.partial ? "partial" : "normal"} · 信心度 {dataset.confidence}% ·{" "}
            {dataset.stopReason}
          </strong>
        </div>
      </div>

      <div className="assessment-evidence">
        <div>
          <h4>命中原因</h4>
          {assessment.reasons.length > 0 ? (
            <ul className="result-reason-list">
              {assessment.reasons.map((reason) => (
                <li key={reason}>{reason}</li>
              ))}
            </ul>
          ) : (
            <p>{hasScore ? "未命中加分規則。" : "沒有足夠 evidence 可評估規則。"}</p>
          )}
        </div>
        <div>
          <h4>節點評估</h4>
          {assessment.nodeAssessments.length > 0 ? (
            <ul className="node-assessment-list">
              {assessment.nodeAssessments.map((node, index) => (
                <li key={node.address || index}>
                  <strong>{node.address}</strong>
                  <span>
                    {node.score}/100 · {node.level}
                  </span>
                  <small>{node.reasons.join(" · ") || "未命中加分規則"}</small>
                </li>
              ))}
            </ul>
          ) : (
            <p>目前結果沒有 address-level assessment。</p>
          )}
        </div>
      </div>

      <small className="dataset-identity">Dataset {dataset.id}</small>
    </div>
  );
}
