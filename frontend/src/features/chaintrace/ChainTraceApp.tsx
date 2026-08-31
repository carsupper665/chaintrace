"use client";

import type { CSSProperties } from "react";
import { useChainTrace } from "@/src/hooks/useChainTrace";
import { AgentPanel } from "@/src/ui/AgentPanel";
import { AnalysisPanel } from "@/src/ui/AnalysisPanel";
import { CaseDialogs } from "@/src/ui/CaseDialogs";
import { CommandBar } from "@/src/ui/CommandBar";
import { WorkspacePanel } from "@/src/ui/WorkspacePanel";
import type { Owner } from "@/src/auth/backend";

export function ChainTraceApp({ owner }: { owner: Owner }) {
  const ui = useChainTrace();

  return (
    <main
      style={
        {
          "--sidebar-width": `${ui.sidebarWidth}px`,
          "--analysis-width": `${ui.analysisWidth}px`,
        } as CSSProperties
      }
      className={`app-shell ${
        ui.isSidebarCollapsed ? "sidebar-collapsed" : ""
      } ${ui.isAnalysisCollapsed ? "analysis-collapsed" : ""} ${
        ui.isSidebarCollapsed && ui.isSidebarHovered ? "sidebar-peek" : ""
      } ${
        ui.isAnalysisCollapsed && ui.isAnalysisHovered ? "analysis-peek" : ""
      } ${ui.isSidebarClosing ? "sidebar-closing" : ""} ${
        ui.isAnalysisClosing ? "analysis-closing" : ""
      } ${ui.isLightMode ? "theme-light" : "theme-dark"} ${
        ui.resizingPanel ? "panel-resizing" : ""
      }`}
    >
      <WorkspacePanel ui={ui} owner={owner} />
      {ui.active ? (
        <>
          <AgentPanel ui={ui} />
          <AnalysisPanel ui={ui} />
        </>
      ) : (
        <section className="empty-investigation-state" aria-live="polite">
          <span className="empty-investigation-mark" aria-hidden="true" />
          <h3>
            {ui.isWorkspaceLoading ? "正在載入調查工作區" : "建立你的第一個調查"}
          </h3>
          <p>
            {ui.workspaceError
              ? ui.workspaceError
              : ui.isWorkspaceLoading
                ? "正在向後端取得這位 Owner 的調查紀錄。"
                : "新增案例後，即可輸入錢包地址並開始建立交易圖譜。"}
          </p>
          {!ui.isWorkspaceLoading && (
            <button
              type="button"
              onClick={() => void ui.createInvestigation()}
              disabled={ui.isWorkspaceMutating}
            >
              <span className="plus-glyph" aria-hidden="true" />
              {ui.isWorkspaceMutating ? "建立中…" : "新增調查"}
            </button>
          )}
          {!ui.isWorkspaceLoading && (
            <>
              {ui.conversationError && (
                <div className="conversation-error" role="alert">
                  {ui.conversationError}
                </div>
              )}
              <CommandBar ui={ui} />
            </>
          )}
        </section>
      )}
      <CaseDialogs ui={ui} />
    </main>
  );
}
