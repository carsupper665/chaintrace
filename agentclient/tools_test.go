package agentclient

import (
	"encoding/json"
	"testing"
	"time"

	"chaintrace/analysis"
	"chaintrace/model/store"
)

const (
	targetAddress  = "TTargetAddress0000000000000000001"
	peerA          = "TPeerAAAAAAAAAAAAAAAAAAAAAAAAAAA2"
	peerB          = "TPeerBBBBBBBBBBBBBBBBBBBBBBBBBBB3"
	peerC          = "TPeerCCCCCCCCCCCCCCCCCCCCCCCCCCC4"
	unknownAddress = "TNeverSeenInThisDataset0000000005"
)

var origin = time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)

// testTransfers is a small dataset: the target pays A and B, A pays some back,
// and C pays A without ever touching the target.
func testTransfers() []store.TRC20Transfer {
	build := func(hash, from, to, amount string, hour int) store.TRC20Transfer {
		return store.TRC20Transfer{
			DatasetID:          "ds1",
			TransactionHash:    hash,
			EventIdentity:      hash + ":0",
			Asset:              "USDT",
			FromAddress:        from,
			ToAddress:          to,
			AmountSmallestUnit: amount,
			Decimals:           6,
			Timestamp:          origin.Add(time.Duration(hour) * time.Hour),
		}
	}
	return []store.TRC20Transfer{
		build("0xaaa", targetAddress, peerA, "100000000", 0),
		build("0xbbb", targetAddress, peerB, "50000000", 1),
		build("0xccc", peerA, targetAddress, "25000000", 2),
		build("0xddd", peerC, peerA, "10000000", 3),
	}
}

func testInvestigation() store.Investigation {
	address := targetAddress
	return store.Investigation{
		ID: "inv1", OwnerID: 7, Title: "Test", Address: &address,
		Network: store.NetworkTRONMainnet, Status: store.InvestigationCompleted,
		TargetLocked: true,
	}
}

// testScope is an Investigation that has already been analysed.
func testScope() turnScope {
	current := testCurrentResult()
	view := newDatasetView("ds1", testTransfers())
	return turnScope{
		ownerID:         7,
		investigationID: "inv1",
		investigation:   testInvestigation(),
		datasetID:       "ds1",
		current:         &current,
		target:          targetAddress,
		network:         store.NetworkTRONMainnet,
		view:            view,
		assessments: map[string]analysis.NodeAssessment{
			targetAddress: {Address: targetAddress, Score: 20, Level: "medium", Reasons: []string{"fan_out"}},
		},
	}
}

// blankScope is a brand new Investigation: no target, no analysis.
func blankScope() turnScope {
	return turnScope{
		ownerID:         7,
		investigationID: "inv-blank",
		investigation: store.Investigation{
			ID: "inv-blank", OwnerID: 7, Title: "新調查任務",
			Network: store.NetworkTRONMainnet, Status: store.InvestigationPending,
		},
		network:     store.NetworkTRONMainnet,
		view:        newDatasetView("", nil),
		assessments: map[string]analysis.NodeAssessment{},
	}
}

func testOptions() Options {
	return Options{ToolMaxRows: 10}.withDefaults()
}

func call(name string, arguments map[string]any) ToolCall {
	return ToolCall{ID: "fc_1", Name: name, Arguments: arguments}
}

func decode[T any](t *testing.T, result ToolResult) T {
	t.Helper()
	var payload T
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("decode tool result %q: %v", result.Content, err)
	}
	return payload
}

func failureCode(t *testing.T, result ToolResult) string {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected an error result, got %q", result.Content)
	}
	return decode[map[string]string](t, result)["code"]
}

// Check 2 is the one that makes a hallucinated address harmless.
func TestAddressOutsideDatasetIsRefused(t *testing.T) {
	for _, name := range []string{ToolGetAddressDetail, ToolExpandNode, ToolGetTransfers} {
		t.Run(name, func(t *testing.T) {
			result := toolRunner{options: testOptions()}.execute(testScope(),
				call(name, map[string]any{"address": unknownAddress}))
			if code := failureCode(t, result); code != codeOutOfScope {
				t.Errorf("code = %q, want %q", code, codeOutOfScope)
			}
		})
	}
}

func TestRefusalKeepsTheCallIdentity(t *testing.T) {
	// Without call_id the Agent cannot match the refusal to its request.
	result := toolRunner{options: testOptions()}.execute(testScope(),
		call(ToolGetAddressDetail, map[string]any{"address": unknownAddress}))
	if result.CallID != "fc_1" || result.Name != ToolGetAddressDetail {
		t.Errorf("result identity = %+v", result)
	}
}

// Check 5: arguments are Agent-generated and cannot be trusted.
func TestInvalidArgumentsAreRejected(t *testing.T) {
	cases := map[string]map[string]any{
		"missing":   {},
		"wrongType": {"address": 42},
		"empty":     {"address": "   "},
		"tooLong":   {"address": string(make([]byte, maximumAddressLen+1))},
	}
	for name, arguments := range cases {
		t.Run(name, func(t *testing.T) {
			result := toolRunner{options: testOptions()}.execute(testScope(), call(ToolGetAddressDetail, arguments))
			if code := failureCode(t, result); code != codeInvalidArgs {
				t.Errorf("code = %q, want %q", code, codeInvalidArgs)
			}
		})
	}
}

func TestUnknownToolIsRejected(t *testing.T) {
	result := toolRunner{options: testOptions()}.execute(testScope(), call("drop_database", map[string]any{}))
	if code := failureCode(t, result); code != codeUnknownTool {
		t.Errorf("code = %q, want %q", code, codeUnknownTool)
	}
}

// A read against an Investigation that has never been analysed must say so and
// point at the fix, not just report the address as out of scope.
func TestReadsBeforeAnyAnalysisExplainWhatToDo(t *testing.T) {
	for _, name := range []string{ToolGetAddressDetail, ToolExpandNode, ToolGetTransfers} {
		t.Run(name, func(t *testing.T) {
			result := toolRunner{options: testOptions()}.execute(
				blankScope(), call(name, map[string]any{"address": targetAddress}))
			if code := failureCode(t, result); code != codeNoAnalysisYet {
				t.Errorf("code = %q, want %q", code, codeNoAnalysisYet)
			}
		})
	}
}

// Starting an analysis with no target must send the Agent to the right tool.
func TestStartAnalysisWithoutTargetIsRefused(t *testing.T) {
	result := toolRunner{options: testOptions()}.execute(
		blankScope(), call(ToolStartAnalysis, map[string]any{}))
	if code := failureCode(t, result); code != codeNoTarget {
		t.Errorf("code = %q, want %q", code, codeNoTarget)
	}
}

// Model-written addresses pass the same Base58Check validation as typed input.
func TestSetTargetRejectsAnInvalidAddress(t *testing.T) {
	result := toolRunner{options: testOptions()}.execute(
		blankScope(), call(ToolSetInvestigationTarget, map[string]any{"address": "TNotARealAddress"}))
	if code := failureCode(t, result); code != codeInvalidArgs {
		t.Errorf("code = %q, want %q", code, codeInvalidArgs)
	}
}

// ADR-0004: the target is immutable once the first analysis has completed.
func TestSetTargetOnALockedInvestigationIsRefused(t *testing.T) {
	result := toolRunner{options: testOptions()}.execute(
		testScope(), call(ToolSetInvestigationTarget,
			map[string]any{"address": "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"}))
	if code := failureCode(t, result); code != codeTargetLocked {
		t.Errorf("code = %q, want %q", code, codeTargetLocked)
	}
}

func TestAddressDetailReportsExactTotals(t *testing.T) {
	result := toolRunner{options: testOptions()}.execute(testScope(),
		call(ToolGetAddressDetail, map[string]any{"address": targetAddress}))
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	detail := decode[addressDetailResult](t, result)
	if detail.SentDisplay != "150 USDT" || detail.ReceivedDisplay != "25 USDT" {
		t.Errorf("totals = %q / %q", detail.SentDisplay, detail.ReceivedDisplay)
	}
	if detail.SentCount != 2 || detail.ReceivedCount != 1 {
		t.Errorf("counts = %d / %d", detail.SentCount, detail.ReceivedCount)
	}
	if detail.DistinctRecipients != 2 || detail.DistinctSenders != 1 {
		t.Errorf("peers = %d / %d", detail.DistinctRecipients, detail.DistinctSenders)
	}
	if !detail.IsTarget {
		t.Error("target address not marked as the Investigation Target")
	}
	if detail.RiskScore == nil || *detail.RiskScore != 20 || detail.RiskLevel != "medium" {
		t.Errorf("assessment = %+v", detail)
	}
}

func TestAddressDetailWithoutAssessmentOmitsScore(t *testing.T) {
	result := toolRunner{options: testOptions()}.execute(testScope(),
		call(ToolGetAddressDetail, map[string]any{"address": peerC}))
	detail := decode[addressDetailResult](t, result)
	if detail.RiskScore != nil {
		t.Errorf("riskScore = %v, want null for an unassessed address", *detail.RiskScore)
	}
	if len(detail.Reasons) != 0 {
		t.Errorf("reasons = %v, want empty", detail.Reasons)
	}
}

func TestGetTransfersFiltersByDirection(t *testing.T) {
	scope, options := testScope(), testOptions()
	incoming := decode[transfersResult](t, toolRunner{options: options}.execute(scope,
		call(ToolGetTransfers, map[string]any{"address": peerA, "direction": "in"})))
	if incoming.Matched != 2 {
		t.Errorf("inbound matched = %d, want 2", incoming.Matched)
	}
	outgoing := decode[transfersResult](t, toolRunner{options: options}.execute(scope,
		call(ToolGetTransfers, map[string]any{"address": peerA, "direction": "out"})))
	if outgoing.Matched != 1 {
		t.Errorf("outbound matched = %d, want 1", outgoing.Matched)
	}
	both := decode[transfersResult](t, toolRunner{options: options}.execute(scope,
		call(ToolGetTransfers, map[string]any{"address": peerA})))
	if both.Matched != 3 {
		t.Errorf("both matched = %d, want 3", both.Matched)
	}
}

func TestGetTransfersRejectsUnknownDirection(t *testing.T) {
	result := toolRunner{options: testOptions()}.execute(testScope(),
		call(ToolGetTransfers, map[string]any{"address": peerA, "direction": "sideways"}))
	if code := failureCode(t, result); code != codeInvalidArgs {
		t.Errorf("code = %q, want %q", code, codeInvalidArgs)
	}
}

func TestGetTransfersAppliesMinimumAmount(t *testing.T) {
	result := decode[transfersResult](t, toolRunner{options: testOptions()}.execute(testScope(),
		call(ToolGetTransfers, map[string]any{"minAmountSmallestUnit": "60000000"})))
	if result.Matched != 1 || result.Transfers[0].TransactionHash != "0xaaa" {
		t.Errorf("result = %+v", result)
	}
}

func TestGetTransfersRejectsNonNumericMinimum(t *testing.T) {
	result := toolRunner{options: testOptions()}.execute(testScope(),
		call(ToolGetTransfers, map[string]any{"minAmountSmallestUnit": "lots"}))
	if code := failureCode(t, result); code != codeInvalidArgs {
		t.Errorf("code = %q, want %q", code, codeInvalidArgs)
	}
}

// Check 3: one tool call cannot flood the model's context.
func TestGetTransfersIsCappedAndSaysSo(t *testing.T) {
	options := Options{ToolMaxRows: 2}.withDefaults()
	result := decode[transfersResult](t, toolRunner{options: options}.execute(testScope(),
		call(ToolGetTransfers, map[string]any{})))
	if result.Returned != 2 || result.Matched != 4 {
		t.Errorf("returned/matched = %d/%d, want 2/4", result.Returned, result.Matched)
	}
	if result.Truncated == "" {
		t.Error("truncation was not disclosed")
	}
	// Sorted by amount, so the cap keeps the largest transfers.
	if result.Transfers[0].TransactionHash != "0xaaa" || result.Transfers[1].TransactionHash != "0xbbb" {
		t.Errorf("kept the wrong transfers: %+v", result.Transfers)
	}
}

func TestRequestedLimitCannotExceedTheSystemCap(t *testing.T) {
	options := Options{ToolMaxRows: 2}.withDefaults()
	result := decode[transfersResult](t, toolRunner{options: options}.execute(testScope(),
		call(ToolGetTransfers, map[string]any{"limit": 100})))
	if result.Returned != 2 {
		t.Errorf("returned = %d, want the system cap of 2", result.Returned)
	}
}

func TestToolSpecsCoverEveryExecutableTool(t *testing.T) {
	declared := make(map[string]bool)
	for _, spec := range ToolSpecs() {
		if spec.Name == "" || spec.Description == "" || spec.Parameters == nil {
			t.Errorf("incomplete tool spec: %+v", spec)
		}
		declared[spec.Name] = true
	}
	for _, name := range []string{
		ToolGetAddressDetail, ToolExpandNode, ToolGetTransfers,
		ToolSetInvestigationTarget, ToolStartAnalysis,
	} {
		if !declared[name] {
			t.Errorf("tool %q is executable but never declared to the Agent", name)
		}
	}
}
