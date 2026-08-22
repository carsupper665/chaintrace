import type { AnalysisScope } from "@/src/middle/investigation-contract";
import {
  MAX_TRANSFER_LIMIT,
  MAX_TRAVERSAL_DEPTH,
} from "@/src/middle/investigation-contract";

// An emptied number input reports "", which Number() turns into 0 — a scope value the
// backend rejects. Keep the last valid value instead.
function numberOr(raw: string, fallback: number) {
  const parsed = Number(raw);
  return raw.trim() === "" || Number.isNaN(parsed) ? fallback : parsed;
}

export function AnalysisScopeControls({
  scope,
  disabled,
  onChange,
}: {
  scope: AnalysisScope;
  disabled: boolean;
  onChange: (scope: AnalysisScope) => void;
}) {
  return (
    <fieldset className="analysis-scope-controls" disabled={disabled}>
      <legend>分析範圍</legend>
      <label>
        <span>Transfer 上限</span>
        <input
          name="transferLimit"
          type="number"
          min={1}
          max={MAX_TRANSFER_LIMIT}
          step={1}
          value={scope.transferLimit}
          onChange={(event) =>
            onChange({
              ...scope,
              transferLimit: numberOr(event.target.value, scope.transferLimit),
            })
          }
        />
        <small>1–{MAX_TRANSFER_LIMIT}</small>
      </label>
      <label>
        <span>追蹤深度</span>
        <input
          name="traversalDepth"
          type="number"
          min={1}
          max={MAX_TRAVERSAL_DEPTH}
          step={1}
          value={scope.traversalDepth}
          onChange={(event) =>
            onChange({
              ...scope,
              traversalDepth: numberOr(event.target.value, scope.traversalDepth),
            })
          }
        />
        <small>1–{MAX_TRAVERSAL_DEPTH} hops</small>
      </label>
      <div className="analysis-window-fixed">
        <span>分析期間</span>
        <strong>固定 30 天</strong>
        <small>run 開始時的 confirmed cutoff 往前計算</small>
      </div>
    </fieldset>
  );
}
