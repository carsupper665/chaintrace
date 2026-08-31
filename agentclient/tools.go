package agentclient

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"chaintrace/analysis"
	"chaintrace/model/store"
)

// Tool failure codes. These reach the Agent as tool results, not as HTTP
// errors, so it can explain the refusal to the analyst.
const (
	codeOutOfScope    = "out_of_scope"
	codeInvalidArgs   = "invalid_arguments"
	codeUnknownTool   = "unknown_tool"
	codeLookupFailed  = "lookup_failed"
	maximumAddressLen = 64
)

// turnScope is the authorization context, resolved once at the start of a turn
// and reused for every tool call in that turn. A turn lasts seconds, which is
// why one lookup is enough (see docs/development-rules.md section 3).
//
// Everything here is derived by Go from its own database. Nothing the Agent
// says about ownership is trusted.
type turnScope struct {
	ownerID         uint
	investigationID string
	investigation   store.Investigation
	target          string
	network         store.InvestigationNetwork

	// datasetID is empty and current is nil until an Analysis Run has published
	// a result. view is never nil; before that it is simply empty, so every
	// in-scope check answers "no" instead of panicking.
	datasetID   string
	current     *analysis.CurrentResult
	view        *datasetView
	assessments map[string]analysis.NodeAssessment
}

// hasAnalysis reports whether there is a published result to reason from.
func (s turnScope) hasAnalysis() bool { return s.current != nil }

// sessionKey identifies the evidence a sidecar session was opened on. Before
// any analysis exists it is the Investigation, so publishing a result changes
// the key and the stale session is rebuilt with the new evidence.
func (s turnScope) sessionKey() string {
	if s.datasetID != "" {
		return s.datasetID
	}
	return "no-analysis:" + s.investigationID
}

// toolRunner executes the Agent's Tool Calls. It carries only what execution
// needs, so tools stay testable without a live sidecar.
type toolRunner struct {
	options Options
	runs    *analysis.RunManager
}

// execute runs one Tool Call after checking it stays inside the scope.
//
// The five checks from docs/development-rules.md section 6:
//  1. scope was derived by Go, never taken from the Agent — see turnScope.
//  2. every address argument must appear in the current dataset.
//  3. every result is capped at options.ToolMaxRows.
//  4. call count and turn deadline are enforced by the caller's loop.
//  5. arguments are Agent-generated text and are validated as untrusted input.
func (r toolRunner) execute(scope turnScope, call ToolCall) ToolResult {
	switch call.Name {
	case ToolGetAddressDetail:
		return withAddress(scope, call, func(address string) ToolResult {
			return jsonResult(call, addressDetail(scope, address))
		})
	case ToolExpandNode:
		return withAddress(scope, call, func(address string) ToolResult {
			page, err := analysis.LoadGraphPage(
				scope.ownerID, scope.investigationID, scope.datasetID, "", address, r.options.ToolMaxRows,
			)
			if err != nil {
				return failure(call, codeLookupFailed, "無法展開這個節點")
			}
			return jsonResult(call, expansion(page))
		})
	case ToolGetTransfers:
		if refusal := requireAnalysis(scope, call); refusal != nil {
			return *refusal
		}
		return getTransfers(scope, r.options, call)
	case ToolSetInvestigationTarget:
		return r.setInvestigationTarget(scope, call)
	case ToolStartAnalysis:
		return r.startAnalysis(scope, call)
	default:
		return failure(call, codeUnknownTool, fmt.Sprintf("不支援的工具: %s", call.Name))
	}
}

// requireAnalysis refuses a read that has nothing to read yet, and says what to
// do instead. Without this the Agent would only learn "address not in scope",
// which is true but useless when no analysis has ever run.
func requireAnalysis(scope turnScope, call ToolCall) *ToolResult {
	if scope.hasAnalysis() {
		return nil
	}
	result := failure(call, codeNoAnalysisYet,
		"這筆調查還沒有分析結果，所以沒有任何鏈上資料可查。"+
			"請先設定目標並呼叫 start_analysis。")
	return &result
}

// withAddress applies checks 5 and 2 before handing a validated address on.
func withAddress(scope turnScope, call ToolCall, run func(string) ToolResult) ToolResult {
	if refusal := requireAnalysis(scope, call); refusal != nil {
		return *refusal
	}
	address, ok := stringArgument(call.Arguments, "address")
	if !ok {
		return failure(call, codeInvalidArgs, "缺少 address 參數或格式不正確")
	}
	if !scope.view.inScope(address) {
		return failure(call, codeOutOfScope,
			"這個地址不在本次分析範圍內。可以照實告訴使用者，不要猜測它的內容。")
	}
	return run(address)
}

type addressDetailResult struct {
	Address            string    `json:"address"`
	IsTarget           bool      `json:"isTarget"`
	SentDisplay        string    `json:"sentDisplay"`
	ReceivedDisplay    string    `json:"receivedDisplay"`
	SentCount          int       `json:"sentCount"`
	ReceivedCount      int       `json:"receivedCount"`
	DistinctRecipients int       `json:"distinctRecipients"`
	DistinctSenders    int       `json:"distinctSenders"`
	FirstSeen          time.Time `json:"firstSeen"`
	LastSeen           time.Time `json:"lastSeen"`
	SampleTransaction  string    `json:"sampleTransaction"`
	RiskScore          *int      `json:"riskScore"`
	RiskLevel          string    `json:"riskLevel"`
	Reasons            []string  `json:"reasons"`
}

func addressDetail(scope turnScope, address string) addressDetailResult {
	result := addressDetailResult{
		Address:  address,
		IsTarget: address == scope.target,
		Reasons:  []string{},
	}
	if stat, ok := scope.view.stats[address]; ok {
		result.SentDisplay = formatExact(stat.sent.String(), stat.decimals, stat.asset)
		result.ReceivedDisplay = formatExact(stat.received.String(), stat.decimals, stat.asset)
		result.SentCount = stat.sentCount
		result.ReceivedCount = stat.receivedCount
		result.DistinctRecipients = len(stat.recipients)
		result.DistinctSenders = len(stat.senders)
		result.FirstSeen = stat.firstAt
		result.LastSeen = stat.lastAt
		result.SampleTransaction = stat.sampleTx
	}
	if assessment, ok := scope.assessments[address]; ok {
		score := assessment.Score
		result.RiskScore = &score
		result.RiskLevel = assessment.Level
		if assessment.Reasons != nil {
			result.Reasons = assessment.Reasons
		}
	}
	return result
}

type expansionResult struct {
	Address   string            `json:"address"`
	Nodes     []string          `json:"relatedAddresses"`
	Edges     []TransferSummary `json:"transfers"`
	HasMore   bool              `json:"hasMore"`
	Truncated string            `json:"note,omitempty"`
}

func expansion(page analysis.GraphPage) expansionResult {
	result := expansionResult{Nodes: make([]string, 0, len(page.Nodes)), Edges: make([]TransferSummary, 0, len(page.Edges))}
	for _, node := range page.Nodes {
		result.Nodes = append(result.Nodes, node.Address)
	}
	for _, edge := range page.Edges {
		result.Edges = append(result.Edges, TransferSummary{
			TransactionHash: edge.TransactionHash,
			From:            edge.From,
			To:              edge.To,
			AmountDisplay:   formatExact(edge.Amount.SmallestUnit, edge.Amount.Decimals, edge.Amount.Asset),
			Timestamp:       edge.Timestamp,
		})
	}
	result.HasMore = page.HasMore
	if page.HasMore {
		result.Truncated = "結果已達單次回傳上限，這不是這個地址的全部關係。"
	}
	return result
}

type transfersResult struct {
	Transfers []TransferSummary `json:"transfers"`
	Matched   int               `json:"matchedCount"`
	Returned  int               `json:"returnedCount"`
	Truncated string            `json:"note,omitempty"`
}

// transferFilter is a validated get_transfers request.
type transferFilter struct {
	address   string
	direction string
	minimum   *big.Int
	limit     int
}

func getTransfers(scope turnScope, options Options, call ToolCall) ToolResult {
	filter, refusal := parseTransferFilter(scope, options, call)
	if refusal != nil {
		return *refusal
	}
	return jsonResult(call, filter.apply(scope.view.transfers))
}

// parseTransferFilter validates the Agent's arguments. It returns a refusal
// rather than an error so the reason reaches the Agent as a tool result.
func parseTransferFilter(
	scope turnScope, options Options, call ToolCall,
) (transferFilter, *ToolResult) {
	reject := func(code, message string) (transferFilter, *ToolResult) {
		result := failure(call, code, message)
		return transferFilter{}, &result
	}

	filter := transferFilter{limit: options.ToolMaxRows}
	if address, ok := stringArgument(call.Arguments, "address"); ok {
		if !scope.view.inScope(address) {
			return reject(codeOutOfScope,
				"這個地址不在本次分析範圍內。可以照實告訴使用者，不要猜測它的內容。")
		}
		filter.address = address
	}

	filter.direction, _ = stringArgument(call.Arguments, "direction")
	switch filter.direction {
	case "", "both", "in", "out":
	default:
		return reject(codeInvalidArgs, "direction 只能是 in、out 或 both")
	}

	if raw, ok := stringArgument(call.Arguments, "minAmountSmallestUnit"); ok {
		minimum, valid := new(big.Int).SetString(raw, 10)
		if !valid {
			return reject(codeInvalidArgs, "minAmountSmallestUnit 必須是整數字串")
		}
		filter.minimum = minimum
	}

	if requested, ok := intArgument(call.Arguments, "limit"); ok && requested > 0 && requested < filter.limit {
		filter.limit = requested
	}
	return filter, nil
}

func (f transferFilter) apply(transfers []store.TRC20Transfer) transfersResult {
	type sized struct {
		transfer store.TRC20Transfer
		amount   *big.Int
	}
	matches := make([]sized, 0)
	for _, transfer := range transfers {
		amount, ok := new(big.Int).SetString(transfer.AmountSmallestUnit, 10)
		if ok && f.matches(transfer, amount) {
			matches = append(matches, sized{transfer: transfer, amount: amount})
		}
	}
	// Largest first, so truncating keeps the transfers most worth citing.
	sort.Slice(matches, func(i, j int) bool {
		comparison := matches[i].amount.Cmp(matches[j].amount)
		if comparison != 0 {
			return comparison > 0
		}
		return matches[i].transfer.TransactionHash < matches[j].transfer.TransactionHash
	})

	result := transfersResult{
		Transfers: make([]TransferSummary, 0, f.limit),
		Matched:   len(matches),
	}
	for _, item := range matches[:min(len(matches), f.limit)] {
		result.Transfers = append(result.Transfers, summarize(item.transfer))
	}
	result.Returned = len(result.Transfers)
	if result.Matched > result.Returned {
		result.Truncated = fmt.Sprintf(
			"符合條件的有 %d 筆，只回傳金額最大的 %d 筆。", result.Matched, result.Returned)
	}
	return result
}

func (f transferFilter) matches(transfer store.TRC20Transfer, amount *big.Int) bool {
	if f.minimum != nil && amount.Cmp(f.minimum) < 0 {
		return false
	}
	if f.address == "" {
		return true
	}
	switch f.direction {
	case "in":
		return transfer.ToAddress == f.address
	case "out":
		return transfer.FromAddress == f.address
	default:
		return transfer.FromAddress == f.address || transfer.ToAddress == f.address
	}
}

// stringArgument treats the Agent's arguments as untrusted text: wrong type,
// empty, or implausibly long all fail the same way.
func stringArgument(arguments map[string]any, key string) (string, bool) {
	raw, ok := arguments[key]
	if !ok {
		return "", false
	}
	text, ok := raw.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maximumAddressLen {
		return "", false
	}
	return text, true
}

func intArgument(arguments map[string]any, key string) (int, bool) {
	raw, ok := arguments[key]
	if !ok {
		return 0, false
	}
	switch value := raw.(type) {
	case float64: // JSON numbers decode as float64
		return int(value), true
	case int:
		return value, true
	default:
		return 0, false
	}
}

func jsonResult(call ToolCall, payload any) ToolResult {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return failure(call, codeLookupFailed, "結果無法序列化")
	}
	return ToolResult{CallID: call.ID, Name: call.Name, Content: string(encoded)}
}

func failure(call ToolCall, code, message string) ToolResult {
	encoded, err := json.Marshal(map[string]string{"code": code, "message": message})
	if err != nil {
		encoded = []byte(`{"code":"internal_error"}`)
	}
	return ToolResult{CallID: call.ID, Name: call.Name, Content: string(encoded), IsError: true}
}
