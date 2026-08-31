package agentclient

import (
	"math/big"
	"sort"
	"time"

	"chaintrace/analysis"
	"chaintrace/model/store"
)

const (
	topCounterparties = 10
	topTransfers      = 20
	ruleExampleLimit  = 3
)

// Evidence is everything an Agent Turn is allowed to reason from. Anything
// absent here is, as far as the Agent is concerned, unknown.
//
// Analysis is nil until an Analysis Run has published a result, so a brand new
// Investigation is a conversable state rather than an error. One nullable
// block is easier for a model to reason about than several independently
// nullable fields.
type Evidence struct {
	Investigation EvidenceInvestigation `json:"investigation"`
	ActiveRun     *EvidenceRun          `json:"activeRun"`
	Analysis      *EvidenceAnalysis     `json:"analysis"`
}

type EvidenceInvestigation struct {
	ID      string                     `json:"id"`
	Title   string                     `json:"title"`
	Address *string                    `json:"address"`
	Network store.InvestigationNetwork `json:"network"`
	Status  store.InvestigationStatus  `json:"status"`
	// TargetLocked reports that the Investigation Target is fixed. Per ADR-0004
	// it becomes immutable once the first analysis completes.
	TargetLocked bool `json:"targetLocked"`
}

// EvidenceRun describes an Analysis Run in flight, so a turn arriving mid-run
// can say so instead of pretending there is nothing happening (ADR-0014).
type EvidenceRun struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	Phase              string `json:"phase,omitempty"`
	CollectedTransfers int    `json:"collectedTransfers"`
	ErrorCode          string `json:"errorCode,omitempty"`
}

// EvidenceAnalysis is the published result an Agent may reason from.
type EvidenceAnalysis struct {
	Dataset    analysis.DatasetResult    `json:"dataset"`
	Metrics    analysis.MetricsResult    `json:"metrics"`
	Assessment analysis.AssessmentResult `json:"assessment"`

	// TopCounterparties ranks addresses that transacted directly with the
	// Investigation Target, by exact moved amount and by transfer count.
	TopCounterpartiesByAmount []Counterparty `json:"topCounterpartiesByAmount"`
	TopCounterpartiesByCount  []Counterparty `json:"topCounterpartiesByCount"`

	// TopTransfers are the largest single transfers anywhere in the dataset,
	// each carrying its transaction hash so the Agent can cite it.
	TopTransfers []TransferSummary `json:"topTransfers"`

	// DailyActivity is the transfer count per UTC day, which is how time
	// clustering becomes visible without shipping every timestamp.
	DailyActivity []DailyActivity `json:"dailyActivity"`

	// RuleExamples grounds each triggered rule in concrete addresses. Without
	// these the Agent can only restate rule names.
	RuleExamples map[string][]RuleExample `json:"ruleExamples"`
}

type Counterparty struct {
	Address string `json:"address"`
	// SentToTarget and ReceivedFromTarget are exact amounts in the asset's
	// smallest unit, with a pre-formatted display string so the Agent never
	// has to do decimal arithmetic itself.
	SentToTarget       analysis.ExactAmount `json:"sentToTarget"`
	ReceivedFromTarget analysis.ExactAmount `json:"receivedFromTarget"`
	TotalDisplay       string               `json:"totalDisplay"`
	TransferCount      int                  `json:"transferCount"`
	SampleTransaction  string               `json:"sampleTransaction"`
}

type TransferSummary struct {
	TransactionHash string    `json:"transactionHash"`
	From            string    `json:"from"`
	To              string    `json:"to"`
	AmountDisplay   string    `json:"amountDisplay"`
	Timestamp       time.Time `json:"timestamp"`
}

type DailyActivity struct {
	Date          string `json:"date"`
	TransferCount int    `json:"transferCount"`
}

type RuleExample struct {
	Address            string `json:"address"`
	Score              int    `json:"score"`
	Level              string `json:"level"`
	DistinctRecipients int    `json:"distinctRecipients"`
	DistinctSenders    int    `json:"distinctSenders"`
	SampleTransaction  string `json:"sampleTransaction"`
}

// addressStat accumulates one address's activity across the whole dataset.
// Amounts stay in big.Int because transfer amounts are stored as exact decimal
// strings; float arithmetic would silently lose precision on large USDT moves.
type addressStat struct {
	sent          *big.Int
	received      *big.Int
	sentCount     int
	receivedCount int
	recipients    map[string]struct{}
	senders       map[string]struct{}
	transferCount int
	sampleTx      string
	decimals      int
	asset         string
	firstAt       time.Time
	lastAt        time.Time
}

func newAddressStat() *addressStat {
	return &addressStat{
		sent:       new(big.Int),
		received:   new(big.Int),
		recipients: make(map[string]struct{}),
		senders:    make(map[string]struct{}),
	}
}

// describeInvestigation is the part of the evidence that always exists, even
// before any analysis has run.
func describeInvestigation(investigation store.Investigation) EvidenceInvestigation {
	return EvidenceInvestigation{
		ID:           investigation.ID,
		Title:        investigation.Title,
		Address:      investigation.Address,
		Network:      investigation.Network,
		Status:       investigation.Status,
		TargetLocked: investigation.TargetLocked,
	}
}

// buildAnalysisEvidence assembles the published-result half of the evidence
// from data already in memory. It touches no database, so it is testable with
// hand-built inputs.
func buildAnalysisEvidence(
	current analysis.CurrentResult, target string, view *datasetView,
) *EvidenceAnalysis {
	byAmount, byCount := rankCounterparties(view.transfers, target, current.Dataset.Asset)
	return &EvidenceAnalysis{
		Dataset:                   current.Dataset,
		Metrics:                   current.Metrics,
		Assessment:                current.Assessment,
		TopCounterpartiesByAmount: byAmount,
		TopCounterpartiesByCount:  byCount,
		TopTransfers:              rankTransfers(view.transfers),
		DailyActivity:             dailyActivity(view.transfers),
		RuleExamples:              ruleExamples(current.Assessment.NodeAssessments, view.stats),
	}
}

func aggregate(transfers []store.TRC20Transfer) map[string]*addressStat {
	stats := make(map[string]*addressStat)
	touch := func(address string) *addressStat {
		stat, ok := stats[address]
		if !ok {
			stat = newAddressStat()
			stats[address] = stat
		}
		return stat
	}
	for _, transfer := range transfers {
		amount, ok := new(big.Int).SetString(transfer.AmountSmallestUnit, 10)
		if !ok {
			// A malformed amount is skipped rather than fatal: the evidence pack
			// is a summary, and the exact rows remain available through tools.
			continue
		}
		from := touch(transfer.FromAddress)
		from.sent.Add(from.sent, amount)
		from.sentCount++
		from.recipients[transfer.ToAddress] = struct{}{}
		observe(from, transfer)

		to := touch(transfer.ToAddress)
		to.received.Add(to.received, amount)
		to.receivedCount++
		to.senders[transfer.FromAddress] = struct{}{}
		observe(to, transfer)
	}
	return stats
}

// observe records the bookkeeping shared by both sides of a transfer.
func observe(stat *addressStat, transfer store.TRC20Transfer) {
	stat.transferCount++
	if stat.sampleTx == "" {
		stat.sampleTx = transfer.TransactionHash
	}
	stat.decimals = transfer.Decimals
	stat.asset = transfer.Asset
	at := transfer.Timestamp.UTC()
	if stat.firstAt.IsZero() || at.Before(stat.firstAt) {
		stat.firstAt = at
	}
	if at.After(stat.lastAt) {
		stat.lastAt = at
	}
}

// counterpartyTotals accumulates one peer's dealings with the Investigation
// Target. Amounts stay in big.Int for the same reason as everywhere else here:
// transfer amounts are exact decimal strings.
type counterpartyTotals struct {
	sentToTarget       *big.Int
	receivedFromTarget *big.Int
	count              int
	decimals           int
	sampleTx           string
}

func (c *counterpartyTotals) record(amount *big.Int, hash string, inbound bool) {
	if inbound {
		c.sentToTarget.Add(c.sentToTarget, amount)
	} else {
		c.receivedFromTarget.Add(c.receivedFromTarget, amount)
	}
	c.count++
	if c.sampleTx == "" {
		c.sampleTx = hash
	}
}

// accumulateCounterparties keeps only transfers with the target on one side.
// Self-transfers are skipped: an address is not its own counterparty.
func accumulateCounterparties(
	transfers []store.TRC20Transfer, target string,
) map[string]*counterpartyTotals {
	peers := make(map[string]*counterpartyTotals)
	touch := func(address string, decimals int) *counterpartyTotals {
		peer, ok := peers[address]
		if !ok {
			peer = &counterpartyTotals{
				sentToTarget:       new(big.Int),
				receivedFromTarget: new(big.Int),
				decimals:           decimals,
			}
			peers[address] = peer
		}
		return peer
	}
	for _, transfer := range transfers {
		amount, ok := new(big.Int).SetString(transfer.AmountSmallestUnit, 10)
		if !ok || transfer.FromAddress == transfer.ToAddress {
			continue
		}
		switch target {
		case transfer.ToAddress:
			touch(transfer.FromAddress, transfer.Decimals).
				record(amount, transfer.TransactionHash, true)
		case transfer.FromAddress:
			touch(transfer.ToAddress, transfer.Decimals).
				record(amount, transfer.TransactionHash, false)
		}
	}
	return peers
}

// rankCounterparties looks only at transfers with the Investigation Target on
// one side, which is what an analyst means by "who did this address deal with".
func rankCounterparties(
	transfers []store.TRC20Transfer, target, asset string,
) (byAmount []Counterparty, byCount []Counterparty) {
	if target == "" {
		return []Counterparty{}, []Counterparty{}
	}
	peers := accumulateCounterparties(transfers, target)

	all := make([]Counterparty, 0, len(peers))
	totals := make(map[string]*big.Int, len(peers))
	for address, peer := range peers {
		total := new(big.Int).Add(peer.sentToTarget, peer.receivedFromTarget)
		totals[address] = total
		all = append(all, Counterparty{
			Address:            address,
			SentToTarget:       analysis.ExactAmount{SmallestUnit: peer.sentToTarget.String(), Decimals: peer.decimals, Asset: asset},
			ReceivedFromTarget: analysis.ExactAmount{SmallestUnit: peer.receivedFromTarget.String(), Decimals: peer.decimals, Asset: asset},
			TotalDisplay:       formatExact(total.String(), peer.decimals, asset),
			TransferCount:      peer.count,
			SampleTransaction:  peer.sampleTx,
		})
	}

	byAmount = append([]Counterparty(nil), all...)
	sort.Slice(byAmount, func(i, j int) bool {
		comparison := totals[byAmount[i].Address].Cmp(totals[byAmount[j].Address])
		if comparison != 0 {
			return comparison > 0
		}
		return byAmount[i].Address < byAmount[j].Address
	})
	byCount = append([]Counterparty(nil), all...)
	sort.Slice(byCount, func(i, j int) bool {
		if byCount[i].TransferCount != byCount[j].TransferCount {
			return byCount[i].TransferCount > byCount[j].TransferCount
		}
		return byCount[i].Address < byCount[j].Address
	})
	return truncate(byAmount, topCounterparties), truncate(byCount, topCounterparties)
}

func rankTransfers(transfers []store.TRC20Transfer) []TransferSummary {
	type sized struct {
		transfer store.TRC20Transfer
		amount   *big.Int
	}
	sizes := make([]sized, 0, len(transfers))
	for _, transfer := range transfers {
		amount, ok := new(big.Int).SetString(transfer.AmountSmallestUnit, 10)
		if !ok {
			continue
		}
		sizes = append(sizes, sized{transfer: transfer, amount: amount})
	}
	sort.Slice(sizes, func(i, j int) bool {
		comparison := sizes[i].amount.Cmp(sizes[j].amount)
		if comparison != 0 {
			return comparison > 0
		}
		return sizes[i].transfer.TransactionHash < sizes[j].transfer.TransactionHash
	})
	result := make([]TransferSummary, 0, topTransfers)
	for _, item := range sizes[:min(len(sizes), topTransfers)] {
		result = append(result, summarize(item.transfer))
	}
	return result
}

func dailyActivity(transfers []store.TRC20Transfer) []DailyActivity {
	counts := make(map[string]int)
	for _, transfer := range transfers {
		counts[transfer.Timestamp.UTC().Format("2006-01-02")]++
	}
	dates := make([]string, 0, len(counts))
	for date := range counts {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	result := make([]DailyActivity, 0, len(dates))
	for _, date := range dates {
		result = append(result, DailyActivity{Date: date, TransferCount: counts[date]})
	}
	return result
}

// ruleExamples pairs each triggered rule with the addresses that actually
// triggered it, plus the counts and a transaction hash the Agent can cite.
func ruleExamples(
	assessments []analysis.NodeAssessment, stats map[string]*addressStat,
) map[string][]RuleExample {
	examples := make(map[string][]RuleExample)
	ordered := append([]analysis.NodeAssessment(nil), assessments...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Score != ordered[j].Score {
			return ordered[i].Score > ordered[j].Score
		}
		return ordered[i].Address < ordered[j].Address
	})
	for _, assessment := range ordered {
		for _, reason := range assessment.Reasons {
			if len(examples[reason]) >= ruleExampleLimit {
				continue
			}
			example := RuleExample{
				Address: assessment.Address,
				Score:   assessment.Score,
				Level:   assessment.Level,
			}
			if stat, ok := stats[assessment.Address]; ok {
				example.DistinctRecipients = len(stat.recipients)
				example.DistinctSenders = len(stat.senders)
				example.SampleTransaction = stat.sampleTx
			}
			examples[reason] = append(examples[reason], example)
		}
	}
	return examples
}
