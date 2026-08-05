import { investigationSuggestions } from "@/src/models/chaintraceData";
import { getRiskTone } from "@/src/utils/riskTone";
import type { ChainTraceController } from "@/src/hooks/useChainTrace";

export function AgentPanel({ ui }: { ui: ChainTraceController }) {
  return (
    <section
      className={`agent-panel ${
        ui.investigations.length === 0 ? "has-no-investigations" : ""
      }`}
    >
      <header className="topbar">
        <div>
          <div className="breadcrumb">
            <span>調查案例</span>
            <b>/</b>
            <strong>{ui.active.id}</strong>
          </div>
          <h2>{ui.active.title}</h2>
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
        {ui.investigations.length === 0 && (
          <section className="empty-investigation-state">
            <span className="empty-investigation-mark" aria-hidden="true" />
            <h3>建立你的第一個調查</h3>
            <p>新增案例後，即可輸入錢包地址並開始建立交易圖譜。</p>
            <button type="button" onClick={ui.createInvestigation}>
              <span className="plus-glyph" aria-hidden="true" />
              新增調查
            </button>
          </section>
        )}
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
                placeholder="輸入 Ethereum 錢包地址（0x…）"
                spellCheck={false}
                autoComplete="off"
                disabled={ui.isGraphLoading}
              />
            </div>
            <button
              type="submit"
              disabled={ui.isGraphLoading || !ui.addressDraft.trim()}
            >
              {ui.isGraphLoading ? "抓取中…" : "確認"}
            </button>
          </form>
          <div className="mini-stat">
            <small>風險分數</small>
            <div className="mini-stat-value">
              <strong className={`risk-text risk-${getRiskTone(ui.active.risk)}`}>
                {ui.active.risk}
              </strong>
              <span>/ 100</span>
            </div>
          </div>
          <div className="mini-stat">
            <small>關聯節點</small>
            <div className="mini-stat-value">
              <strong>{ui.active.relatedNodes}</strong>
              <span>個地址</span>
            </div>
          </div>
          <div className="mini-stat total-flow-stat">
            <small>總資金流</small>
            <div className="mini-stat-value">
              <strong className="total-flow-value">
                {ui.active.totalFlow.toLocaleString()}
                <span> {ui.active.flowAsset || "—"}</span>
              </strong>
            </div>
          </div>
        </div>

        <section className="conversation">
          <div className="agent-intro">
            <div className="agent-mark">AI</div>
            <div>
              <span className="eyebrow">INVESTIGATION AGENT</span>
              <h3>今天要調查什麼？</h3>
              <p>
                描述目標地址或可疑交易模式，我會自動規劃查詢、圖譜展開、
                異常偵測與風險解釋流程。
              </p>
            </div>
          </div>

          <div className="suggestion-grid">
            {investigationSuggestions.map((suggestion, index) => (
              <button
                key={suggestion}
                onClick={() => ui.runInvestigation(suggestion)}
              >
                <span>0{index + 1}</span>
                {suggestion}
                <b>↗</b>
              </button>
            ))}
          </div>

          {ui.chatMessages.length > 0 && (
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
                  <p>{message.content}</p>
                </div>
              ))}
            </div>
          )}

          {ui.isRunning && (
            <div className="agent-connecting" role="status">
              <i />
              正在連線至 AI Agent 後端…
            </div>
          )}
        </section>
      </div>

      <form
        className="command-bar"
        onSubmit={(event) => {
          event.preventDefault();
          void ui.runInvestigation();
        }}
      >
        <button type="button" aria-label="附加調查資料">
          ＋
        </button>
        <textarea
          value={ui.query}
          onChange={(event) => ui.setQuery(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              void ui.runInvestigation();
            }
          }}
          placeholder="輸入調查需求，例如：追蹤此地址流向混幣服務的所有路徑…"
          rows={1}
          aria-label="輸入調查需求"
        />
        <div className="command-meta">
          <span>⌘ Enter 執行</span>
          <button
            className="send-button"
            type="submit"
            aria-label="執行調查"
            disabled={ui.isRunning || !ui.query.trim()}
          >
            ↑
          </button>
        </div>
      </form>
    </section>
  );
}
