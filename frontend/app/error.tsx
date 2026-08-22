"use client";

// Without an error boundary any thrown render error blanks the whole workspace.
export default function WorkspaceError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <main className="workspace-error-boundary" role="alert">
      <h1>工作區發生錯誤</h1>
      <p>這個畫面無法顯示，既有的調查資料沒有被更動。</p>
      {error.digest ? <p>錯誤代碼：{error.digest}</p> : null}
      <button type="button" onClick={reset}>
        重新載入工作區
      </button>
    </main>
  );
}
