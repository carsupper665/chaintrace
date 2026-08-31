package agentclient

import (
	"errors"

	"chaintrace/analysis"
	"chaintrace/controller"
	"chaintrace/model"
)

// Failure codes for the investigation tools. Each one tells the Agent what to
// do next, because a refusal it cannot act on is just a dead end.
const (
	codeNoAnalysisYet = "no_analysis_yet"
	codeNoTarget      = "no_target"
	codeTargetLocked  = "target_locked"
	codeRunActive     = "analysis_already_running"
	codeRunFailed     = "analysis_start_failed"
)

// setInvestigationTarget points the Investigation at an address.
//
// The address arrives as model-written text, so it passes exactly the same
// Base58Check validation as an Owner's typed input, and the immutability rule
// from ADR-0004 is enforced by the same query the REST route uses.
func (r toolRunner) setInvestigationTarget(scope turnScope, call ToolCall) ToolResult {
	address, ok := stringArgument(call.Arguments, "address")
	if !ok {
		return failure(call, codeInvalidArgs, "缺少 address 參數或格式不正確")
	}
	if !controller.IsValidTRONAddress(address) {
		return failure(call, codeInvalidArgs,
			"這不是合法的 TRON Base58Check 地址。請向使用者確認地址，不要自己猜一個。")
	}
	if scope.investigation.TargetLocked {
		return failure(call, codeTargetLocked,
			"這筆調查的目標已經鎖定，不能更換。要查別的地址請告訴使用者開一筆新的調查。")
	}

	updated, err := model.UpdateInvestigation(
		scope.ownerID, scope.investigationID, map[string]any{"address": address}, true,
	)
	if errors.Is(err, model.ErrInvestigationTargetImmutable) {
		return failure(call, codeTargetLocked,
			"這筆調查的目標已經鎖定或正在分析中，現在不能更換。")
	}
	if err != nil {
		return failure(call, codeLookupFailed, "無法設定調查目標")
	}
	return jsonResult(call, map[string]any{
		"address": updated.Address,
		"network": updated.Network,
		"note":    "目標已設定。接下來可以呼叫 start_analysis 開始蒐集資料。",
	})
}

// startAnalysis begins collection and returns immediately.
//
// Per ADR-0014 the turn must not wait for the run: the note tells the Agent to
// answer now, which is the difference between a five-second reply and a turn
// that burns its whole budget polling.
func (r toolRunner) startAnalysis(scope turnScope, call ToolCall) ToolResult {
	// The Investigation is re-read rather than taken from the turn's snapshot:
	// a common opening is set_investigation_target followed by start_analysis
	// in the same turn, and the snapshot predates that write. Authorization
	// still comes from the scope — only the Investigation's own state is fresh.
	// The nil check keeps this usable from unit tests, where the package-level
	// handle is unset and gorm would panic rather than return an error.
	investigation := scope.investigation
	if model.DB != nil {
		if fresh, err := model.GetInvestigation(scope.ownerID, scope.investigationID); err == nil {
			investigation = *fresh
		}
	}

	// The missing target is checked first because it is the actionable one: the
	// Agent can fix it, whereas an absent run manager is a deployment fault.
	if investigation.Address == nil || *investigation.Address == "" {
		return failure(call, codeNoTarget,
			"這筆調查還沒有目標地址。請先呼叫 set_investigation_target，或向使用者要地址。")
	}
	if r.runs == nil {
		return failure(call, codeRunFailed, "分析服務目前無法使用")
	}

	run, err := r.runs.Start(scope.ownerID, investigation, analysis.DefaultScope())
	if errors.Is(err, analysis.ErrAnalysisRunActive) {
		return failure(call, codeRunActive,
			"已經有一個分析正在進行。不要重複啟動，直接告訴使用者目前正在跑。")
	}
	if err != nil {
		return failure(call, codeRunFailed, "無法啟動分析")
	}
	return jsonResult(call, map[string]any{
		"runId":          run.ID,
		"status":         run.Status,
		"transferLimit":  run.TransferLimit,
		"traversalDepth": run.TraversalDepth,
		"note": "分析已開始，需要數分鐘。" +
			"這一輪到此為止：直接告訴使用者已經開始蒐集，完成後會自動給結果。" +
			"不要再呼叫任何工具，也不要嘗試等待。",
	})
}
