package analysis_test

import (
	"chaintrace/analysis"
	"context"
	"math/big"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestRulesV1FanOutFiresOnceAtTenUniqueRecipients(t *testing.T) {
	cutoff := collectorCutoff()
	transfers := make([]analysis.TRC20Transfer, 0, 20)
	for i := 0; i < 9; i++ {
		transfers = append(transfers, collectorTransfer(string(rune('a'+i)), collectorTarget, "recipient-"+string(rune('a'+i)), "1", cutoff.Add(-time.Hour)))
	}
	below := evaluateRules(t, collectorTarget, collectorTransaction("fan-out", cutoff.Add(-time.Hour), transfers...))
	assertEvaluation(t, below, 0, "low", nil)

	transfers = append(transfers, collectorTransfer("tenth", collectorTarget, "recipient-tenth", "1", cutoff.Add(-time.Hour)))
	for i := 0; i < 10; i++ {
		transfers = append(transfers, collectorTransfer("other-"+string(rune('a'+i)), "other", "other-recipient-"+string(rune('a'+i)), "1", cutoff.Add(-time.Hour)))
	}
	atBoundary := evaluateRules(t, collectorTarget, collectorTransaction("fan-out", cutoff.Add(-time.Hour), transfers...))
	assertEvaluation(t, atBoundary, 20, "low", []string{analysis.ReasonFanOut})
	assertNode(t, atBoundary, collectorTarget, 20, []string{analysis.ReasonFanOut})
	assertNode(t, atBoundary, "other", 20, []string{analysis.ReasonFanOut})
}

func TestRulesV1FanInFiresAtTenUniqueSenders(t *testing.T) {
	cutoff := collectorCutoff()
	transfers := make([]analysis.TRC20Transfer, 0, 10)
	for i := 0; i < 10; i++ {
		transfers = append(transfers, collectorTransfer(string(rune('a'+i)), "sender-"+string(rune('a'+i)), collectorTarget, "1", cutoff.Add(-time.Hour)))
	}
	result := evaluateRules(t, collectorTarget, collectorTransaction("fan-in", cutoff.Add(-time.Hour), transfers...))
	assertEvaluation(t, result, 15, "low", []string{analysis.ReasonFanIn})
	assertNode(t, result, collectorTarget, 15, []string{analysis.ReasonFanIn})
}

func TestRulesV1RapidForwardingUsesExactEightyPercentAndSixtyMinuteBoundaries(t *testing.T) {
	cutoff := collectorCutoff()
	received := "10000000000000000000000000000000000000000"
	receivedInt, _ := new(big.Int).SetString(received, 10)
	eightyPercent := new(big.Int).Mul(receivedInt, big.NewInt(80))
	eightyPercent.Quo(eightyPercent, big.NewInt(100))

	fixture := func(amount string, delay time.Duration) analysis.Evaluation {
		return evaluateRules(t, collectorTarget,
			collectorTransaction("incoming", cutoff.Add(-2*time.Hour), collectorTransfer("in", "sender", "forwarder", received, cutoff.Add(-2*time.Hour))),
			collectorTransaction("outgoing", cutoff.Add(-2*time.Hour+delay), collectorTransfer("out", "forwarder", "recipient", amount, cutoff.Add(-2*time.Hour+delay))),
		)
	}
	belowAmount := new(big.Int).Sub(eightyPercent, big.NewInt(1)).String()
	assertEvaluation(t, fixture(belowAmount, 60*time.Minute), 0, "low", nil)
	assertEvaluation(t, fixture(eightyPercent.String(), 60*time.Minute+time.Nanosecond), 0, "low", nil)
	atBoundary := fixture(eightyPercent.String(), 60*time.Minute)
	assertEvaluation(t, atBoundary, 25, "medium", []string{analysis.ReasonRapidForwarding})
	assertNode(t, atBoundary, "forwarder", 25, []string{analysis.ReasonRapidForwarding})
}

func TestRulesV1TargetConnectedForwardingUsesSeventyPercentAndTwentyFourHourBoundaries(t *testing.T) {
	cutoff := collectorCutoff()
	fixture := func(amount string, delay time.Duration) analysis.Evaluation {
		return evaluateRules(t, collectorTarget,
			collectorTransaction("hop-1", cutoff.Add(-26*time.Hour), collectorTransfer("one", collectorTarget, "middle", "100", cutoff.Add(-26*time.Hour))),
			collectorTransaction("hop-2", cutoff.Add(-26*time.Hour+delay), collectorTransfer("two", "middle", "end", amount, cutoff.Add(-26*time.Hour+delay))),
		)
	}
	assertEvaluation(t, fixture("69", 24*time.Hour), 0, "low", nil)
	assertEvaluation(t, fixture("70", 24*time.Hour+time.Nanosecond), 0, "low", nil)
	atBoundary := fixture("70", 24*time.Hour)
	assertEvaluation(t, atBoundary, 25, "medium", []string{analysis.ReasonTargetConnectedForwarding})
	for _, address := range []string{collectorTarget, "middle", "end"} {
		assertNode(t, atBoundary, address, 25, []string{analysis.ReasonTargetConnectedForwarding})
	}
}

func TestRulesV1CircularFlowRequiresTwoToFourHops(t *testing.T) {
	cutoff := collectorCutoff()
	selfLoop := evaluateRules(t, collectorTarget,
		collectorTransaction("self", cutoff.Add(-time.Hour), collectorTransfer("self", "loop", "loop", "1", cutoff.Add(-time.Hour))),
	)
	assertEvaluation(t, selfLoop, 0, "low", nil)

	cycle := evaluateRules(t, collectorTarget,
		collectorTransaction("one", cutoff.Add(-3*time.Hour), collectorTransfer("one", "alpha", "bravo", "1", cutoff.Add(-3*time.Hour))),
		collectorTransaction("two", cutoff.Add(-time.Hour), collectorTransfer("two", "bravo", "alpha", "1", cutoff.Add(-time.Hour))),
	)
	assertEvaluation(t, cycle, 15, "low", []string{analysis.ReasonCircularFlow})
	assertNode(t, cycle, "alpha", 15, []string{analysis.ReasonCircularFlow})
	assertNode(t, cycle, "bravo", 15, []string{analysis.ReasonCircularFlow})
}

func TestRulesV1SeverityBoundariesAndScoreCap(t *testing.T) {
	cutoff := collectorCutoff()
	high := evaluateRules(t, collectorTarget,
		collectorTransaction("incoming", cutoff.Add(-2*time.Hour), collectorTransfer("in", "sender", collectorTarget, "100", cutoff.Add(-2*time.Hour))),
		collectorTransaction("first", cutoff.Add(-90*time.Minute), collectorTransfer("first", collectorTarget, "middle", "80", cutoff.Add(-90*time.Minute))),
		collectorTransaction("second", cutoff.Add(-time.Hour), collectorTransfer("second", "middle", "end", "56", cutoff.Add(-time.Hour))),
	)
	assertEvaluation(t, high, 50, "high", []string{analysis.ReasonRapidForwarding, analysis.ReasonTargetConnectedForwarding})

	criticalBoundaryTransfers := make([]analysis.TRC20Transfer, 0, 20)
	for i := 0; i < 10; i++ {
		suffix := string(rune('a' + i))
		criticalBoundaryTransfers = append(criticalBoundaryTransfers,
			collectorTransfer("in-"+suffix, "boundary-sender-"+suffix, collectorTarget, "100", cutoff.Add(-4*time.Hour)),
			collectorTransfer("out-"+suffix, collectorTarget, "boundary-recipient-"+suffix, "1", cutoff.Add(-2*time.Hour)),
		)
	}
	criticalBoundary := evaluateRules(t, collectorTarget,
		collectorTransaction("boundary-fan", cutoff.Add(-2*time.Hour), criticalBoundaryTransfers...),
		collectorTransaction("boundary-layer", cutoff.Add(-30*time.Minute), collectorTransfer("layer", "boundary-recipient-a", "boundary-end", "1", cutoff.Add(-30*time.Minute))),
		collectorTransaction("boundary-cycle-1", cutoff.Add(-5*time.Hour), collectorTransfer("one", "boundary-cycle-a", "boundary-cycle-b", "1", cutoff.Add(-5*time.Hour))),
		collectorTransaction("boundary-cycle-2", cutoff.Add(-3*time.Hour), collectorTransfer("two", "boundary-cycle-b", "boundary-cycle-a", "1", cutoff.Add(-3*time.Hour))),
	)
	assertEvaluation(t, criticalBoundary, 75, "critical", []string{
		analysis.ReasonFanOut,
		analysis.ReasonFanIn,
		analysis.ReasonTargetConnectedForwarding,
		analysis.ReasonCircularFlow,
	})

	all := allRulesFixture(t, cutoff)
	assertEvaluation(t, all, 100, "critical", []string{
		analysis.ReasonFanOut,
		analysis.ReasonFanIn,
		analysis.ReasonRapidForwarding,
		analysis.ReasonTargetConnectedForwarding,
		analysis.ReasonCircularFlow,
	})
}

func TestRulesV1OutputIsIndependentOfTransactionAndTransferOrder(t *testing.T) {
	cutoff := collectorCutoff()
	transactions := allRulesTransactions(cutoff)
	forward := evaluateRules(t, collectorTarget, transactions...)

	reversed := append([]analysis.BlockchainTransaction(nil), transactions...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	for i := range reversed {
		for left, right := 0, len(reversed[i].Transfers)-1; left < right; left, right = left+1, right-1 {
			reversed[i].Transfers[left], reversed[i].Transfers[right] = reversed[i].Transfers[right], reversed[i].Transfers[left]
		}
	}
	backward := evaluateRules(t, collectorTarget, reversed...)
	if !reflect.DeepEqual(forward, backward) {
		t.Errorf("order changed evaluation:\nforward: %#v\nreverse: %#v", forward, backward)
	}
	if !sort.SliceIsSorted(forward.NodeAssessments, func(i, j int) bool {
		return forward.NodeAssessments[i].Address < forward.NodeAssessments[j].Address
	}) {
		t.Errorf("node assessments are not stable: %#v", forward.NodeAssessments)
	}
}

func TestRulesV1ReturnsInsufficientEvidenceForZeroTransfers(t *testing.T) {
	result, err := (analysis.RulesV1Evaluator{}).Evaluate(context.Background(), analysis.EvaluationInput{})
	if err != nil {
		t.Fatalf("evaluate zero evidence: %v", err)
	}
	if result.Score != nil || result.Level != "" || result.Source != "rules-v1" || len(result.Reasons) != 0 || len(result.NodeAssessments) != 0 {
		t.Errorf("zero-evidence evaluation = %#v", result)
	}
}

func allRulesFixture(t *testing.T, cutoff time.Time) analysis.Evaluation {
	return evaluateRules(t, collectorTarget, allRulesTransactions(cutoff)...)
}

func allRulesTransactions(cutoff time.Time) []analysis.BlockchainTransaction {
	transfers := make([]analysis.TRC20Transfer, 0, 20)
	for i := 0; i < 10; i++ {
		suffix := string(rune('a' + i))
		transfers = append(transfers, collectorTransfer("out-"+suffix, collectorTarget, "recipient-"+suffix, "100", cutoff.Add(-time.Hour)))
		transfers = append(transfers, collectorTransfer("in-"+suffix, "sender-"+suffix, collectorTarget, "100", cutoff.Add(-2*time.Hour)))
	}
	return []analysis.BlockchainTransaction{
		collectorTransaction("fan", cutoff.Add(-time.Hour), transfers...),
		collectorTransaction("layer", cutoff.Add(-30*time.Minute), collectorTransfer("layer", "recipient-a", "layer-end", "70", cutoff.Add(-30*time.Minute))),
		collectorTransaction("cycle-1", cutoff.Add(-3*time.Hour), collectorTransfer("cycle-1", "cycle-a", "cycle-b", "1", cutoff.Add(-3*time.Hour))),
		collectorTransaction("cycle-2", cutoff.Add(-150*time.Minute), collectorTransfer("cycle-2", "cycle-b", "cycle-a", "1", cutoff.Add(-150*time.Minute))),
	}
}

func evaluateRules(t *testing.T, target string, transactions ...analysis.BlockchainTransaction) analysis.Evaluation {
	t.Helper()
	result, err := (analysis.RulesV1Evaluator{}).Evaluate(context.Background(), analysis.EvaluationInput{
		TargetAddress: target,
		Collection:    analysis.Collection{Transactions: transactions},
	})
	if err != nil {
		t.Fatalf("evaluate rules-v1: %v", err)
	}
	return result
}

func assertEvaluation(t *testing.T, result analysis.Evaluation, score int, level string, reasons []string) {
	t.Helper()
	if result.Score == nil || *result.Score != score || result.Level != level || result.Source != "rules-v1" {
		t.Errorf("evaluation score/level/source = %#v, want %d/%s/rules-v1", result, score, level)
	}
	if reasons == nil {
		reasons = []string{}
	}
	if !reflect.DeepEqual(result.Reasons, reasons) {
		t.Errorf("evaluation reasons = %#v, want %#v", result.Reasons, reasons)
	}
}

func assertNode(t *testing.T, result analysis.Evaluation, address string, score int, reasons []string) {
	t.Helper()
	for _, node := range result.NodeAssessments {
		if node.Address != address {
			continue
		}
		if node.Score != score || !reflect.DeepEqual(node.Reasons, reasons) {
			t.Errorf("node %s = %#v, want score %d reasons %#v", address, node, score, reasons)
		}
		return
	}
	t.Errorf("node assessment %s missing from %#v", address, result.NodeAssessments)
}
