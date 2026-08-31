package agentclient

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"chaintrace/analysis"
	"chaintrace/model/store"
)

func testCurrentResult() analysis.CurrentResult {
	score := 60
	return analysis.CurrentResult{
		Dataset: analysis.DatasetResult{
			ID: "ds1", Network: store.NetworkTRONMainnet, Asset: "USDT",
			TransferLimit: 500, TraversalDepth: 2, CollectedTransfers: 4,
			Partial: true, Confidence: 60, StopReason: analysis.StopReasonTransferLimitReached,
		},
		Metrics: analysis.MetricsResult{RelatedNodes: 4, TransferCount: 4},
		Assessment: analysis.AssessmentResult{
			Score: &score, Level: "high", Reasons: []string{"fan_out", "rapid_forwarding"},
			NodeAssessments: []analysis.NodeAssessment{
				{Address: targetAddress, Score: 20, Level: "medium", Reasons: []string{"fan_out"}},
				{Address: peerA, Score: 45, Level: "medium", Reasons: []string{"fan_out", "rapid_forwarding"}},
			},
			Source: "rules-v1",
		},
	}
}

func testEvidence() EvidenceAnalysis {
	view := newDatasetView("ds1", testTransfers())
	return *buildAnalysisEvidence(testCurrentResult(), targetAddress, view)
}

func TestEvidenceCarriesCoverageAndAssessment(t *testing.T) {
	evidence := testEvidence()
	// Without these the Agent cannot honestly state the analysis boundary.
	if !evidence.Dataset.Partial || evidence.Dataset.StopReason == "" {
		t.Errorf("coverage missing: %+v", evidence.Dataset)
	}
	if evidence.Assessment.Score == nil || *evidence.Assessment.Score != 60 {
		t.Errorf("score = %v", evidence.Assessment.Score)
	}
}

func TestCounterpartiesOnlyCountTransfersTouchingTheTarget(t *testing.T) {
	evidence := testEvidence()
	// peerC only ever paid peerA, so it is not a counterparty of the target.
	for _, counterparty := range evidence.TopCounterpartiesByAmount {
		if counterparty.Address == peerC {
			t.Fatalf("peerC is not a counterparty of the target: %+v", counterparty)
		}
	}
	if len(evidence.TopCounterpartiesByAmount) != 2 {
		t.Fatalf("counterparties = %d, want 2", len(evidence.TopCounterpartiesByAmount))
	}
	// peerA moved 100 out plus 25 back = 125; peerB only 50.
	first := evidence.TopCounterpartiesByAmount[0]
	if first.Address != peerA || first.TotalDisplay != "125 USDT" {
		t.Errorf("top counterparty = %+v", first)
	}
	if first.TransferCount != 2 {
		t.Errorf("peerA transfer count = %d, want 2", first.TransferCount)
	}
}

func TestCounterpartyAmountsStayExact(t *testing.T) {
	evidence := testEvidence()
	for _, counterparty := range evidence.TopCounterpartiesByAmount {
		if counterparty.Address != peerA {
			continue
		}
		// Exact smallest-unit values must survive alongside the display string.
		if counterparty.ReceivedFromTarget.SmallestUnit != "100000000" {
			t.Errorf("receivedFromTarget = %q", counterparty.ReceivedFromTarget.SmallestUnit)
		}
		if counterparty.SentToTarget.SmallestUnit != "25000000" {
			t.Errorf("sentToTarget = %q", counterparty.SentToTarget.SmallestUnit)
		}
	}
}

func TestTargetlessInvestigationYieldsEmptyCounterparties(t *testing.T) {
	view := newDatasetView("ds1", testTransfers())
	evidence := buildAnalysisEvidence(testCurrentResult(), "", view)
	// Must serialise as [] rather than null so the Agent sees "none", not "missing".
	encoded, err := json.Marshal(evidence.TopCounterpartiesByAmount)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != "[]" {
		t.Errorf("encoded = %s, want []", encoded)
	}
}

func TestTopTransfersAreSortedByAmountAndCarryHashes(t *testing.T) {
	evidence := testEvidence()
	if len(evidence.TopTransfers) != 4 {
		t.Fatalf("transfers = %d, want 4", len(evidence.TopTransfers))
	}
	previous := new(big.Int)
	for index, transfer := range evidence.TopTransfers {
		if transfer.TransactionHash == "" {
			t.Errorf("transfer %d has no hash to cite", index)
		}
		amount, _ := new(big.Int).SetString(strings.ReplaceAll(
			strings.TrimSuffix(transfer.AmountDisplay, " USDT"), ",", ""), 10)
		if index > 0 && amount != nil && previous.Cmp(amount) < 0 {
			t.Errorf("transfers are not sorted descending at %d", index)
		}
		if amount != nil {
			previous = amount
		}
	}
	if evidence.TopTransfers[0].TransactionHash != "0xaaa" {
		t.Errorf("largest transfer = %+v", evidence.TopTransfers[0])
	}
}

// Rule examples are what let the Agent cite an address instead of restating a
// rule name.
func TestRuleExamplesGroundEveryTriggeredRule(t *testing.T) {
	evidence := testEvidence()
	for _, reason := range evidence.Assessment.Reasons {
		examples := evidence.RuleExamples[reason]
		if len(examples) == 0 {
			t.Fatalf("rule %q has no example address", reason)
		}
		for _, example := range examples {
			if example.Address == "" || example.SampleTransaction == "" {
				t.Errorf("rule %q example is not citable: %+v", reason, example)
			}
		}
	}
	// Highest-scoring address first, so the Agent leads with the strongest case.
	if evidence.RuleExamples["fan_out"][0].Address != peerA {
		t.Errorf("fan_out examples = %+v", evidence.RuleExamples["fan_out"])
	}
}

func TestRuleExamplesAreCapped(t *testing.T) {
	current := testCurrentResult()
	for index := range 6 {
		current.Assessment.NodeAssessments = append(current.Assessment.NodeAssessments,
			analysis.NodeAssessment{
				Address: peerB, Score: index, Level: "low", Reasons: []string{"fan_out"},
			})
	}
	view := newDatasetView("ds1", testTransfers())
	evidence := buildAnalysisEvidence(current, targetAddress, view)
	if len(evidence.RuleExamples["fan_out"]) > ruleExampleLimit {
		t.Errorf("fan_out examples = %d, want at most %d",
			len(evidence.RuleExamples["fan_out"]), ruleExampleLimit)
	}
}

func TestDailyActivityIsChronological(t *testing.T) {
	evidence := testEvidence()
	if len(evidence.DailyActivity) != 1 {
		t.Fatalf("daily activity = %+v, want a single day", evidence.DailyActivity)
	}
	if evidence.DailyActivity[0].Date != "2026-08-01" || evidence.DailyActivity[0].TransferCount != 4 {
		t.Errorf("daily activity = %+v", evidence.DailyActivity[0])
	}
}

func TestFormatExactNeverUsesFloatingPoint(t *testing.T) {
	cases := []struct {
		smallest string
		decimals int
		want     string
	}{
		{"100000000", 6, "100 USDT"},
		{"1", 6, "0.000001 USDT"},
		// 25 digits: far past float64's exact range, and the fraction survives.
		{"1234567890123456789012345", 6, "1,234,567,890,123,456,789.012345 USDT"},
		{"0", 6, "0 USDT"},
		{"1500000", 6, "1.5 USDT"},
		{"250", 0, "250 USDT"},
	}
	for _, test := range cases {
		if got := formatExact(test.smallest, test.decimals, "USDT"); got != test.want {
			t.Errorf("formatExact(%q, %d) = %q, want %q", test.smallest, test.decimals, got, test.want)
		}
	}
}

func TestMalformedAmountsAreSkippedNotFatal(t *testing.T) {
	transfers := append(testTransfers(), store.TRC20Transfer{
		DatasetID: "ds1", TransactionHash: "0xbad", EventIdentity: "0xbad:0",
		FromAddress: peerB, ToAddress: peerC, AmountSmallestUnit: "not-a-number",
		Decimals: 6, Asset: "USDT", Timestamp: origin,
	})
	view := newDatasetView("ds1", transfers)
	evidence := buildAnalysisEvidence(testCurrentResult(), targetAddress, view)
	if len(evidence.TopTransfers) != 4 {
		t.Errorf("transfers = %d, want the 4 well-formed ones", len(evidence.TopTransfers))
	}
	// The address is still in scope: tools can still return its exact rows.
	if !view.inScope(peerC) {
		t.Error("an address with a malformed transfer dropped out of scope")
	}
}
