import { investigationSuggestions } from "@/src/models/chaintraceData";
import { getRiskTone } from "@/src/utils/riskTone";
import type { ChainTraceController } from "@/src/hooks/useChainTrace";
import { formatExactAmount } from "@/src/middle/investigation-client";
import { AnalysisScopeControls } from "@/src/ui/AnalysisScopeControls";
import { CommandBar } from "@/src/ui/CommandBar";

function conversationContent(message: {
  role: "user" | "system" | "agent";
  content: string;
}) {
  if (message.role !== "system") return message.content;
  try {
    const event = JSON.parse(message.content) as { code?: unknown };
    if (event.code === "agent_unavailable") {
      return "Agent 目前無法使用；訊息已保存。";
    }
  } catch {
    // Plain-text system messages are valid conversation content.
  }
  return message.content;
}

export function AgentPanel({ ui }: { ui: ChainTraceController }) {
  const active = ui.active;
  if (!active) return null;
  const currentResult = ui.currentAnalysisResult;
  const assessment = currentResult?.assessment;
  const risk = currentResult
    ? assessment?.score ?? null
    : typeof active.risk === "number"
      ? active.risk
      : null;
  const hasStableResult = Boolean(active.currentResult);
  const relatedNodes = currentResult?.metrics.relatedNodes ?? active.relatedNodes;
  const transferCount =
    currentResult?.metrics.transferCount ?? active.transactionCount;
  const totalFlow = currentResult?.metrics.totalFlow ?? active.totalFlow;
  const isAnalysisActive =
    ui.isAnalysisLoading ||
    active.status === "分析中" ||
    ui.analysisRun?.status === "queued" ||
    ui.analysisRun?.status === "running";
  const analysisStatus = ui.analysisRun
    ? ui.analysisRun.status === "queued"
      ? "分析已排入佇列"
      : ui.analysisRun.status === "running"
        ? `正在分析${ui.analysisRun.phase ? `：${ui.analysisRun.phase}` : ""}，已收集 ${ui.analysisRun.collectedTransfers} 筆 Transfer`
        : ui.analysisRun.status === "completed"
          ? "分析完成，已載入目前結果。"
          : ui.analysisRun.status === "cancelled"
            ? "分析已取消，未發布新結果。"
            : "分析失敗。"
    : "";
  const startAnalysisLabel = isAnalysisActive
    ? "分析進行中…"
    : ui.analysisOutcome === "run_lost"
      ? "重新送出分析"
      : ui.analysisOutcome === "cancelled" || ui.analysisOutcome === "failed"
        ? "重試分析"
        : hasStableResult
          ? "重新分析"
          : "開始分析";

  return (
    <section className="agent-panel">
      <header className="topbar">
        <div>
          <div className="breadcrumb">
            <span>調查案例</span>
            <b>/</b>
            <strong>{active.id}</strong>
          </div>
          <h2>{active.title}</h2>
        </div>
        <div className="topbar-actions">
          <button
            className="theme-toggle"
            onClick={ui.toggleTheme}
            aria-label={
              ui.isLightMode ? "切換成夜間模式" : "切換成日間模式"
            }
            title={ui.isLightMode ? "切換成夜間模式" : "切換成日間模式"}
          >
            <span>{ui.isLightMode ? "☾" : "☀"}</span>
          </button>
        </div>
      </header>

      <div className="agent-content">
        <div className="case-summary">
          <form
            className="address-block address-entry"
            onSubmit={(event) => {
              event.preventDefault();
              void ui.confirmInvestigationAddress();
            }}
          >
            <span className="network-icon" aria-hidden="true">
              <span className="wallet-glyph" />
            </span>
            <div>
              <label htmlFor="investigation-address">調查目標</label>
              <input
                id="investigation-address"
                value={ui.addressDraft}
                onChange={(event) => ui.setAddressDraft(event.target.value)}
                placeholder="輸入 TRON Base58Check 地址（T…）"
                spellCheck={false}
                autoComplete="off"
                readOnly={active.targetLocked}
                disabled={ui.isGraphLoading}
              />
            </div>
            <div className="target-actions">
              <button
                type="submit"
                disabled={
                  ui.isGraphLoading ||
                  active.targetLocked ||
                  !ui.addressDraft.trim()
                }
              >
                {active.targetLocked
                  ? "目標已鎖定"
                  : ui.isGraphLoading
                    ? "儲存中…"
                    : "儲存目標"}
              </button>
              <button
                className="start-analysis-button"
                type="button"
                onClick={() => void ui.startAnalysis()}
                disabled={!active.address || isAnalysisActive}
              >
                {startAnalysisLabel}
              </button>
              {isAnalysisActive && (
                <button
                  className="cancel-analysis-button"
                  type="button"
                  onClick={() => void ui.cancelAnalysis()}
                  disabled={ui.isAnalysisCancelling}
                >
                  {ui.isAnalysisCancelling ? "取消中…" : "取消分析"}
                </button>
              )}
            </div>
          </form>
          {active.targetLocked && (
            <p className="target-lock-note">
              第一次成功分析後，調查目標已永久鎖定；新地址需建立新的 Investigation。
            </p>
          )}
          {(ui.graphError || ui.targetMessage) && (
            <div
              className={`target-feedback ${ui.graphError ? "error" : "success"}`}
              role={ui.graphError ? "alert" : "status"}
            >
              {ui.graphError || ui.targetMessage}
            </div>
          )}
          {(analysisStatus || ui.analysisError) && (
            <div
              className={`analysis-feedback ${ui.analysisError ? "error" : ""}`}
              role={ui.analysisError ? "alert" : "status"}
            >
              {ui.analysisError ||
                (isAnalysisActive && hasStableResult
                  ? `重新分析進行中，既有結果會保留到新結果發布。${analysisStatus}`
                  : analysisStatus)}
            </div>
          )}
          <AnalysisScopeControls
            scope={ui.analysisScope}
            disabled={isAnalysisActive}
            onChange={ui.setAnalysisScope}
          />
          <div className="mini-stat">
            <small>風險分數</small>
            <div className="mini-stat-value">
              <strong
                className={
                  risk === null
                    ? "risk-text risk-pending"
                    : `risk-text risk-${getRiskTone(risk)}`
                }
              >
                {risk ?? "—"}
              </strong>
              <span>
                {risk === null
                  ? hasStableResult
                    ? "證據不足"
                    : "待分析"
                  : assessment?.level
                    ? `/ 100 · ${assessment.level}`
                    : "/ 100"}
              </span>
            </div>
          </div>
          <div className="mini-stat">
            <small>關聯節點</small>
            <div className="mini-stat-value">
              <strong>{relatedNodes ?? "—"}</strong>
              <span>個地址</span>
            </div>
          </div>
          <div className="mini-stat">
            <small>Transfer 數</small>
            <div className="mini-stat-value">
              <strong>{transferCount ?? "—"}</strong>
              <span>筆</span>
            </div>
          </div>
          <div className="mini-stat total-flow-stat">
            <small>總資金流</small>
            <div className="mini-stat-value">
              <strong className="total-flow-value">
                {totalFlow ? formatExactAmount(totalFlow) : "—"}
                <span> {totalFlow?.asset || ""}</span>
              </strong>
            </div>
          </div>
        </div>

        <section className="conversation">
          <div className="agent-intro">
            <div className="agent-mark">AI</div>
            <div>
              <span className="eyebrow">INVESTIGATION AGENT</span>
              <h3>調查對話</h3>
              <p>
                Command 與 system outcome 會保存到這筆 Investigation；Agent
                provider 未設定時不會產生模擬回覆。
              </p>
            </div>
          </div>

          <div className="summary-actions">
            <button
              type="button"
              className="generate-summary-button"
              onClick={() => void ui.generateSummary()}
              disabled={
                !hasStableResult ||
                ui.isSummarizing ||
                ui.isRunning ||
                ui.isConversationLoading
              }
              title={
                hasStableResult
                  ? "依目前的分析結果產生摘要"
                  : "需要先完成一次分析"
              }
            >
              {ui.isSummarizing ? "產生摘要中…" : "產生調查摘要"}
            </button>
            <small>
              {hasStableResult
                ? "摘要依目前分析結果產生，同一份結果只會產生一次。"
                : "完成一次分析後才能產生摘要。"}
            </small>
          </div>

          {ui.summaryNotice && (
            <div className="summary-notice" role="status">
              {ui.summaryNotice}
            </div>
          )}

          <div className="suggestion-grid">
            {investigationSuggestions.map((suggestion, index) => (
              <button
                key={suggestion}
                onClick={() => ui.runInvestigation(suggestion)}
                disabled={ui.isRunning || ui.isConversationLoading}
              >
                <span>0{index + 1}</span>
                {suggestion}
                <b>↗</b>
              </button>
            ))}
          </div>

          {(ui.hasMoreConversation || ui.chatMessages.length > 0) && (
            <div className="chat-thread" aria-live="polite">
              {ui.chatMessages.map((message) => (
                <div
                  className={`chat-message ${message.role}`}
                  key={message.id}
                >
                  <span>
                    {message.role === "user"
                      ? "你"
                      : message.role === "agent"
                        ? "AI"
                        : "!"}
                  </span>
                  <p>{conversationContent(message)}</p>
                </div>
              ))}
              {/* Backend conversation pages walk forward from the oldest
                  message, so more history is disclosed below the thread. */}
              {ui.hasMoreConversation && (
                <button
                  type="button"
                  className="load-more-conversation"
                  onClick={() => void ui.loadMoreConversation()}
                  disabled={ui.isConversationLoading}
                >
                  {ui.isConversationLoading ? "載入中…" : "載入更多訊息"}
                </button>
              )}
            </div>
          )}

          {ui.conversationError && (
            <div className="conversation-error" role="alert">
              {ui.conversationError}
            </div>
          )}

          {ui.isConversationLoading && !ui.hasMoreConversation && (
            <div className="agent-connecting" role="status">
              <i />
              正在載入已保存的對話…
            </div>
          )}

          {ui.isRunning && (
            <div className="agent-connecting" role="status">
              <i />
              正在保存 command 與 system outcome…
            </div>
          )}
        </section>
      </div>

      <CommandBar ui={ui} />
    </section>
  );
}
