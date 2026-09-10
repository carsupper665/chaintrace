import { useEffect, useRef } from "react";
import type { ChainTraceController } from "@/src/hooks/useChainTrace";
import { getRiskTone } from "@/src/utils/riskTone";
import type { Owner } from "@/src/auth/backend";
import { formatExactAmount } from "@/src/middle/investigation-client";

export function WorkspacePanel({
  ui,
  owner,
}: {
  ui: ChainTraceController;
  owner: Owner;
}) {
  const ownerName = owner.display_name?.trim() || owner.username;
  const initials = ownerName.slice(0, 2).toUpperCase();
  const searchInputRef = useRef<HTMLInputElement>(null);
  const { isSidebarCollapsed, toggleSidebarPin } = ui;

  useEffect(() => {
    function focusSearch(event: KeyboardEvent) {
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== "k") {
        return;
      }
      event.preventDefault();
      if (isSidebarCollapsed) toggleSidebarPin();
      window.requestAnimationFrame(() => searchInputRef.current?.focus());
    }

    window.addEventListener("keydown", focusSearch);
    return () => window.removeEventListener("keydown", focusSearch);
  }, [isSidebarCollapsed, toggleSidebarPin]);

  return (
    <aside
      className="workspace-panel"
      onMouseEnter={ui.enterSidebar}
      onMouseLeave={ui.leaveSidebar}
    >
      <div className="brand-row">
        <button
          className="icon-button menu-button"
          aria-label={
            ui.isSidebarCollapsed ? "展開調查工作區" : "收合調查工作區"
          }
          aria-expanded={!ui.isSidebarCollapsed}
          onClick={ui.toggleSidebarPin}
        >
          <span className="panel-toggle-glyph panel-toggle-left" />
        </button>
        <div>
          <span className="eyebrow">CHAINTRACE</span>
          <h1>調查工作區</h1>
        </div>
        <button
          className="icon-button add-button"
          aria-label="新增調查任務"
          onClick={() => void ui.createInvestigation()}
          disabled={ui.isWorkspaceMutating}
        >
          <span className="plus-glyph" aria-hidden="true" />
        </button>
      </div>

      <div className="collapsed-sidebar-tools" aria-label="調查工作區快捷操作">
        <button
          className="rail-icon-button rail-search-icon"
          aria-label="展開並搜尋調查"
          onClick={ui.toggleSidebarPin}
        />
        <button
          className="rail-icon-button rail-folder-icon"
          aria-label="展開調查工作區"
          onClick={ui.toggleSidebarPin}
        />
        <button
          className="rail-icon-button rail-add-icon"
          aria-label="新增調查任務"
          onClick={() => void ui.createInvestigation()}
        />
      </div>

      <label className="search-box">
        <span>⌕</span>
        <input
          ref={searchInputRef}
          value={ui.search}
          onChange={(event) => ui.setSearch(event.target.value)}
          placeholder="搜尋調查或 TRON 地址"
          aria-label="搜尋調查或 TRON 地址"
        />
        <kbd>⌘ K</kbd>
      </label>

      <div className="section-label">
        <span>調查案例</span>
        <strong>{ui.investigations.length}</strong>
        <button
          type="button"
          className="workspace-reload-button"
          onClick={() => void ui.reloadInvestigations()}
          disabled={ui.isWorkspaceLoading}
        >
          {ui.isWorkspaceLoading ? "載入中" : "重新載入"}
        </button>
      </div>

      <nav className="investigation-list" aria-label="調查任務">
        {!ui.isWorkspaceLoading && ui.filtered.length === 0 && (
          <div className="workspace-empty">
            <span className="empty-folder-icon" aria-hidden="true" />
            <strong>
              {ui.search ? "找不到符合的調查" : "尚無調查案例"}
            </strong>
            <small>
              {ui.search
                ? "請調整標題或 TRON 地址關鍵字。"
                : "按右上角新增第一個調查，紀錄會由後端保存。"}
            </small>
          </div>
        )}
        {ui.filtered.map((item) => (
          <button
            key={item.id}
            className={`investigation-item ${
              ui.activeId === item.id ? "active" : ""
            }`}
            onClick={() => void ui.setActiveId(item.id)}
            onContextMenu={(event) => {
              event.preventDefault();
              void ui.setActiveId(item.id);
              ui.setContextMenu({
                id: item.id,
                x: Math.min(event.clientX, window.innerWidth - 190),
                y: Math.min(event.clientY, window.innerHeight - 116),
              });
            }}
            onKeyDown={(event) => {
              if (event.shiftKey && event.key === "F10") {
                event.preventDefault();
                const rect = event.currentTarget.getBoundingClientRect();
                ui.setContextMenu({
                  id: item.id,
                  x: rect.left + 36,
                  y: rect.bottom - 4,
                });
              }
            }}
            title="右鍵可重新命名或刪除"
          >
            <span className={`status-dot ${item.status}`} />
            <span className="investigation-copy">
              <strong>{item.title}</strong>
              <small>
                {item.address || "尚未指定地址"} · TRON
              </small>
            </span>
            {typeof item.risk === "number" ? (
              <span className={`risk-pill risk-${getRiskTone(item.risk)}`}>
                {item.risk}
              </span>
            ) : item.currentResult ? (
              <span className="risk-pill risk-pending">證據不足</span>
            ) : null}
            {item.currentResult && (
              <span className="investigation-result-summary">
                {item.transactionCount ?? "—"} Transfer · {item.relatedNodes ?? "—"} 地址
                {item.totalFlow && (
                  <small>
                    {formatExactAmount(item.totalFlow)} {item.totalFlow.asset}
                  </small>
                )}
              </span>
            )}
          </button>
        ))}
      </nav>
      {ui.workspaceError && (
        <div className="workspace-error" role="alert">
          {ui.workspaceError}
        </div>
      )}

      <div className="workspace-footer">
        <div className="system-status">
          <span className="network-mark">T</span>
          <div>
            <strong>支援網路</strong>
            <small>TRON mainnet · TRC20 USDT</small>
          </div>
        </div>
        <div className="user-card">
          <span className="avatar">{initials}</span>
          <span className="owner-copy">
            <strong>{ownerName}</strong>
          </span>
          <form action="/api/auth/logout" method="post">
            <button className="logout-button" type="submit">
              登出
            </button>
          </form>
        </div>
      </div>
      {!ui.isSidebarCollapsed && (
        <div
          className="panel-resize-handle sidebar-resize-handle"
          role="separator"
          aria-label="調整調查工作區寬度"
          aria-orientation="vertical"
          tabIndex={0}
          onPointerDown={(event) => {
            event.preventDefault();
            event.stopPropagation();
            event.currentTarget.setPointerCapture(event.pointerId);
            ui.beginPanelResize("sidebar", event.clientX);
          }}
          onPointerMove={(event) => {
            if (ui.resizingPanel === "sidebar") {
              ui.updatePanelResize(event.clientX);
            }
          }}
          onPointerUp={(event) => {
            if (event.currentTarget.hasPointerCapture(event.pointerId)) {
              event.currentTarget.releasePointerCapture(event.pointerId);
            }
            ui.endPanelResize();
          }}
          onPointerCancel={ui.endPanelResize}
          onDoubleClick={() => ui.resetPanelWidth("sidebar")}
          onKeyDown={(event) => {
            if (event.key === "ArrowLeft") {
              event.preventDefault();
              ui.adjustPanelWidth("sidebar", -20);
            } else if (event.key === "ArrowRight") {
              event.preventDefault();
              ui.adjustPanelWidth("sidebar", 20);
            } else if (event.key === "Home") {
              event.preventDefault();
              ui.toggleSidebarPin();
            } else if (event.key === "End") {
              event.preventDefault();
              ui.resetPanelWidth("sidebar");
            }
          }}
        />
      )}
    </aside>
  );
}
