package agentclient

// The tool contract the Agent is given. Go owns it because Go executes the
// tools; the Agent only asks. Execution and the checks around it live in
// tools.go.

// Tier 1 tools read data already inside the Analysis Dataset. They are cheap
// and the Agent may call them on its own.
const (
	ToolGetAddressDetail = "get_address_detail"
	ToolExpandNode       = "expand_node"
	ToolGetTransfers     = "get_transfers"

	// Investigation tools change the Investigation itself. The Agent may run
	// them because an Owner asking for an investigation has already decided
	// (ADR-0013); the backend still enforces every constraint.
	ToolSetInvestigationTarget = "set_investigation_target"
	ToolStartAnalysis          = "start_analysis"
)

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ToolResult struct {
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

func addressSchema(description string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"address": map[string]any{"type": "string", "description": description},
		},
		"required":             []string{"address"},
		"additionalProperties": false,
	}
}

// ToolSpecs is the tool contract sent to the Agent. Go owns it because Go
// executes the tools.
func ToolSpecs() []ToolSpec {
	return []ToolSpec{
		{
			Name:        ToolGetAddressDetail,
			Description: "取得某個地址在本次分析範圍內的進出統計、往來對象數量，以及它觸發了哪些風險規則。",
			Parameters:  addressSchema("要查詢的地址，必須是本次分析範圍內出現過的地址。"),
		},
		{
			Name: ToolExpandNode,
			Description: "展開某個地址在本次分析範圍內的資金往來關係。只揭露已收集的資料，" +
				"不會去鏈上抓新資料，也不會更動這筆調查的目標——目標鎖定不影響這個工具。",
			Parameters: addressSchema("要展開的地址，必須是本次分析範圍內出現過的地址。"),
		},
		{
			Name: ToolGetTransfers,
			Description: "在本次分析範圍內查詢轉帳明細。" +
				"可依地址、方向與最小金額篩選，結果依金額由大到小排序。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"address": map[string]any{
						"type":        "string",
						"description": "只看與這個地址有關的轉帳。省略則查全部。",
					},
					"direction": map[string]any{
						"type":        "string",
						"enum":        []string{"in", "out", "both"},
						"description": "搭配 address 使用：in 只看轉入，out 只看轉出，預設 both。",
					},
					"minAmountSmallestUnit": map[string]any{
						"type":        "string",
						"description": "最小金額，以資產的最小單位表示的整數字串。",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "最多回傳幾筆。超過系統上限時以系統上限為準。",
					},
				},
				"additionalProperties": false,
			},
		},
		{
			Name: ToolSetInvestigationTarget,
			Description: "設定這筆調查整體要追查的地址。" +
				"只有在調查還沒鎖定目標時可用；第一次分析完成後目標就固定了，換地址要開新的調查。" +
				"想查看分析結果裡的其他地址，用 expand_node 或 get_address_detail，不要用這個。",
			Parameters: addressSchema("要追查的 TRON Base58Check 地址（T 開頭）。"),
		},
		{
			Name: ToolStartAnalysis,
			Description: "對目前的調查目標啟動一次鏈上資料蒐集與風險評估。" +
				"需要先設定好目標。這是非同步的：呼叫後會立刻回傳，蒐集要跑數分鐘，" +
				"**不要等它完成，也不要重複呼叫**，直接告訴使用者已經開始。",
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
	}
}
