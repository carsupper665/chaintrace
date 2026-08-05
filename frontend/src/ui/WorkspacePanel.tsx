import type { ChainTraceController } from "@/src/hooks/useChainTrace";
import { getRiskTone } from "@/src/utils/riskTone";

export function WorkspacePanel({ ui }: { ui: ChainTraceController }) {
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
          onClick={ui.createInvestigation}
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
          onClick={ui.createInvestigation}
        />
      </div>

      <label className="search-box">
        <span>⌕</span>
        <input
          value={ui.search}
          onChange={(event) => ui.setSearch(event.target.value)}
          placeholder="搜尋案例或地址"
          aria-label="搜尋案例或地址"
        />
        <kbd>⌘ K</kbd>
      </label>

      <div className="section-label">
        <span>調查案例</span>
        <strong>{ui.investigations.length}</strong>
      </div>

      <nav className="investigation-list" aria-label="調查任務">
        {ui.filtered.length === 0 && (
          <div className="workspace-empty">
            <span className="empty-folder-icon" aria-hidden="true" />
            <strong>尚無調查案例</strong>
            <small>按右上角新增第一個調查</small>
          </div>
        )}
        {ui.filtered.map((item) => (
          <button
            key={item.id}
            className={`investigation-item ${
              ui.activeId === item.id ? "active" : ""
            }`}
            onClick={() => ui.setActiveId(item.id)}
            onContextMenu={(event) => {
              event.preventDefault();
              ui.setActiveId(item.id);
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
                {item.address || "尚未指定地址"} · {item.network}
              </small>
            </span>
            {item.risk > 0 && (
              <span className={`risk-pill risk-${getRiskTone(item.risk)}`}>
                {item.risk}
              </span>
            )}
          </button>
        ))}
      </nav>

      <div className="workspace-footer">
        <div className="system-status">
          <span className="pulse-dot" />
          <div>
            <strong>分析服務正常</strong>
            <small>Ethereum · Bitcoin</small>
          </div>
        </div>
        <div className="user-card">
          <span className="avatar">SL</span>
          <span>
            <strong>shlee</strong>
            <small>研究分析員</small>
          </span>
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
