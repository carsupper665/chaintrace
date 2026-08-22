import type { ChainTraceController } from "@/src/hooks/useChainTrace";

export function CaseDialogs({ ui }: { ui: ChainTraceController }) {
  return (
    <>
      {ui.contextMenu && (
        <div
          className="case-context-menu"
          role="menu"
          aria-label="案例操作"
          style={{ left: ui.contextMenu.x, top: ui.contextMenu.y }}
          onClick={(event) => event.stopPropagation()}
        >
          <div className="context-case-name">
            {
              ui.investigations.find(
                (item) => item.id === ui.contextMenu?.id,
              )?.title
            }
          </div>
          <button
            role="menuitem"
            onClick={() => {
              const item = ui.investigations.find(
                (candidate) => candidate.id === ui.contextMenu?.id,
              );
              if (item) ui.openRename(item);
            }}
          >
            <span>✎</span>
            變更名稱
          </button>
          <button
            className="danger-action"
            role="menuitem"
            title="刪除此案例"
            onClick={() => {
              const item = ui.investigations.find(
                (candidate) => candidate.id === ui.contextMenu?.id,
              );
              if (item) ui.setDeleteTarget(item);
              ui.setContextMenu(null);
            }}
          >
            <span>×</span>
            刪除案例
          </button>
        </div>
      )}

      {ui.renameTarget && (
        <div className="modal-backdrop" role="presentation">
          <section
            className="case-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="rename-title"
          >
            <button
              className="modal-close"
              onClick={() => ui.setRenameTarget(null)}
              aria-label="關閉"
            >
              ×
            </button>
            <span className="eyebrow">INVESTIGATION</span>
            <h2 id="rename-title">變更案例名稱</h2>
            <p>
              案例編號 {ui.renameTarget.id} 與既有分析結果不會改變。
            </p>
            <form onSubmit={ui.renameInvestigation}>
              <label>
                案例名稱
                <input
                  autoFocus
                  required
                  maxLength={48}
                  value={ui.renameValue}
                  onChange={(event) => ui.setRenameValue(event.target.value)}
                  placeholder="輸入新的案例名稱"
                />
              </label>
              <div className="modal-actions">
                <button
                  className="secondary-button"
                  type="button"
                  onClick={() => ui.setRenameTarget(null)}
                >
                  取消
                </button>
                <button
                  className="primary-button"
                  type="submit"
                  disabled={ui.isWorkspaceMutating}
                >
                  {ui.isWorkspaceMutating ? "儲存中…" : "儲存名稱"}
                </button>
              </div>
            </form>
          </section>
        </div>
      )}

      {ui.deleteTarget && (
        <div className="modal-backdrop" role="presentation">
          <section
            className="case-modal delete-modal"
            role="alertdialog"
            aria-modal="true"
            aria-labelledby="delete-title"
          >
            <button
              className="modal-close"
              onClick={() => ui.setDeleteTarget(null)}
              aria-label="關閉"
            >
              ×
            </button>
            <span className="delete-symbol">×</span>
            <span className="eyebrow danger-eyebrow">DELETE CASE</span>
            <h2 id="delete-title">刪除這個調查案例？</h2>
            <p>
              「{ui.deleteTarget.title}」將從目前工作區移除，此操作無法復原。
            </p>
            <div className="modal-actions">
              <button
                className="secondary-button"
                onClick={() => ui.setDeleteTarget(null)}
              >
                保留案例
              </button>
              <button
                className="delete-button"
                onClick={() => void ui.deleteInvestigation()}
                disabled={ui.isWorkspaceMutating}
              >
                {ui.isWorkspaceMutating ? "刪除中…" : "確認刪除"}
              </button>
            </div>
          </section>
        </div>
      )}
    </>
  );
}
