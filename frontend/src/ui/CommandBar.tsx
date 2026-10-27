import { useEffect, useId, useRef, useState } from "react";
import type { ChainTraceController } from "@/src/hooks/useChainTrace";

const CLAIM_IMAGE_ACCEPT = "image/png,image/jpeg,image/webp";

function isClaimImage(file: File) {
  return (
    file.type === "image/png" ||
    file.type === "image/jpeg" ||
    file.type === "image/webp"
  );
}

// The same command bar serves an open Investigation and an empty workspace. A
// command sent with nothing open creates its own Investigation, so the Owner
// can start by pasting an address and asking for an investigation.
export function CommandBar({ ui }: { ui: ChainTraceController }) {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const attachRef = useRef<HTMLDivElement>(null);
  const menuId = useId();
  const [menuOpen, setMenuOpen] = useState(false);
  const hasInvestigation = Boolean(ui.active);
  const isBusy =
    ui.isRunning || ui.isConversationLoading || ui.isClaimExtracting;
  const canStageImage =
    !ui.active?.targetLocked && !ui.isClaimExtracting && !ui.isGraphLoading;
  const canSend =
    !isBusy &&
    !ui.isWorkspaceMutating &&
    (Boolean(ui.query.trim()) || Boolean(ui.composerImage));

  function stageImage(file: File | null | undefined) {
    if (!file || !isClaimImage(file) || !canStageImage) return;
    ui.stageComposerImage(file);
  }

  function openImagePicker() {
    setMenuOpen(false);
    if (!canStageImage) return;
    fileInputRef.current?.click();
  }

  useEffect(() => {
    if (!menuOpen) return;

    function onPointerDown(event: MouseEvent) {
      if (!attachRef.current?.contains(event.target as Node)) {
        setMenuOpen(false);
      }
    }

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setMenuOpen(false);
    }

    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [menuOpen]);

  const attachDisabledReason = ui.active?.targetLocked
    ? "調查目標已鎖定，無法再附加截圖"
    : ui.isClaimExtracting
      ? "正在辨識截圖…"
      : ui.isGraphLoading
        ? "圖資料載入中，請稍候"
        : null;

  return (
    <form
      className="command-bar"
      onSubmit={(event) => {
        event.preventDefault();
        void ui.sendComposer();
      }}
    >
      {ui.composerImageUrl && (
        <div className="composer-attachments">
          <div className="composer-attachment">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src={ui.composerImageUrl} alt="待傳送截圖" />
            <button
              type="button"
              className="composer-attachment-remove"
              aria-label="移除截圖"
              onClick={() => ui.clearComposerImage()}
            >
              ×
            </button>
          </div>
        </div>
      )}
      <div className="command-bar-row">
        <div className="command-attach" ref={attachRef}>
          <button
            type="button"
            className="command-attach-trigger"
            aria-label="附加調查資料"
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            aria-controls={menuOpen ? menuId : undefined}
            title="附加調查資料"
            disabled={!canStageImage}
            onClick={() => setMenuOpen((open) => !open)}
          >
            ＋
          </button>
          {menuOpen && (
            <div
              id={menuId}
              className="command-attach-menu"
              role="menu"
              aria-label="附加調查資料"
            >
              <button
                type="button"
                role="menuitem"
                className="command-attach-item"
                disabled={!canStageImage}
                title={attachDisabledReason ?? undefined}
                onClick={openImagePicker}
              >
                <span className="command-attach-icon" aria-hidden="true">
                  ⌗
                </span>
                <span className="command-attach-copy">
                  <strong>上傳截圖</strong>
                  <small>
                    {attachDisabledReason ??
                      "附加後會在聊天中請你確認地址（可能看錯）"}
                  </small>
                </span>
              </button>
            </div>
          )}
        </div>
        <input
          ref={fileInputRef}
          id="claim-screenshot"
          className="sr-only"
          type="file"
          accept={CLAIM_IMAGE_ACCEPT}
          tabIndex={-1}
          disabled={!canStageImage}
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = "";
            stageImage(file);
          }}
        />
        <textarea
          value={ui.query}
          onChange={(event) => ui.setQuery(event.target.value)}
          onPaste={(event) => {
            const items = event.clipboardData?.items;
            if (!items) return;
            for (const item of items) {
              if (!item.type.startsWith("image/")) continue;
              const file = item.getAsFile();
              if (!file || !isClaimImage(file)) continue;
              event.preventDefault();
              stageImage(file);
              return;
            }
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              void ui.sendComposer();
            }
          }}
          placeholder={
            ui.pendingAddressConfirmation
              ? "回覆「對」確認地址，或「不是」重新提供…"
              : hasInvestigation
                ? "輸入調查需求，或貼上截圖讓我辨識地址…"
                : "貼上 TRON 地址／截圖並說明需求（會自動建立新調查）"
          }
          rows={1}
          aria-label="輸入調查需求"
        />
        <div className="command-meta">
          <span>
            {ui.pendingAddressConfirmation
              ? "請確認地址是否看對"
              : hasInvestigation
                ? "Enter 送出 · Shift+Enter 換行 · 可貼圖"
                : "Enter 送出後會自動建立調查"}
          </span>
          {ui.agentModels.length > 0 && (
            <select
              className="model-select"
              aria-label="選擇模型"
              value={ui.selectedModel}
              onChange={(event) => ui.setSelectedModel(event.target.value)}
              disabled={isBusy}
            >
              <option value="">預設模型</option>
              {ui.agentModels.map((model) => (
                <option key={model.id} value={model.id}>
                  {model.displayName}
                </option>
              ))}
            </select>
          )}
          <button
            className="send-button"
            type="submit"
            aria-label="送出"
            disabled={!canSend}
          >
            ↑
          </button>
        </div>
      </div>
    </form>
  );
}
