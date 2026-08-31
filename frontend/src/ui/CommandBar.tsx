import type { ChainTraceController } from "@/src/hooks/useChainTrace";

// The same command bar serves an open Investigation and an empty workspace. A
// command sent with nothing open creates its own Investigation, so the Owner
// can start by pasting an address and asking for an investigation.
export function CommandBar({ ui }: { ui: ChainTraceController }) {
  const hasInvestigation = Boolean(ui.active);
  const isBusy = ui.isRunning || ui.isConversationLoading;

  return (
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
        placeholder={
          hasInvestigation
            ? "輸入調查需求，例如：追蹤此地址流向混幣服務的所有路徑…"
            : "貼上 TRON 地址並說明需求，例如：幫我調查 TR7NHq… （會自動建立新調查）"
        }
        rows={1}
        aria-label="輸入調查需求"
      />
      <div className="command-meta">
        <span>
          {hasInvestigation
            ? "Enter 執行 · Shift+Enter 換行"
            : "Enter 送出後會自動建立調查"}
        </span>
        <button
          className="send-button"
          type="submit"
          aria-label="執行調查"
          disabled={isBusy || !ui.query.trim()}
        >
          ↑
        </button>
      </div>
    </form>
  );
}
