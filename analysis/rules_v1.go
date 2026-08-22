package analysis

import (
	"context"
	"errors"
	"math/big"
	"sort"
	"time"
)

const (
	ReasonFanOut                    = "fan_out"
	ReasonFanIn                     = "fan_in"
	ReasonRapidForwarding           = "rapid_forwarding"
	ReasonTargetConnectedForwarding = "target_connected_forwarding"
	ReasonCircularFlow              = "circular_flow"

	rulesV1FanOutRecipients        = 10
	rulesV1FanInSenders            = 10
	rulesV1RapidForwardNumerator   = 80
	rulesV1RapidForwardDenominator = 100
	rulesV1RapidForwardWindow      = 60 * time.Minute
	rulesV1PathForwardNumerator    = 70
	rulesV1PathForwardDenominator  = 100
	rulesV1PathForwardWindow       = 24 * time.Hour
	rulesV1MinimumCycleHops        = 2
	rulesV1MaximumCycleHops        = 4
	rulesV1MaximumScore            = 100

	rulesV1FanOutWeight       = 20
	rulesV1FanInWeight        = 15
	rulesV1RapidForwardWeight = 25
	rulesV1PathForwardWeight  = 25
	rulesV1CircularFlowWeight = 15
)

type RulesV1Evaluator struct{}

type rulesV1Transfer struct {
	key    string
	from   string
	to     string
	amount *big.Int
	at     time.Time
}

type rulesV1Signal struct {
	reason     string
	weight     int
	qualifying map[string]struct{}
}

func (RulesV1Evaluator) Evaluate(ctx context.Context, input EvaluationInput) (Evaluation, error) {
	transfers, err := normalizeRulesV1Transfers(input.Collection)
	if err != nil {
		return Evaluation{}, err
	}
	if len(transfers) == 0 {
		return Evaluation{
			Reasons:         []string{},
			NodeAssessments: []NodeAssessment{},
			Source:          "rules-v1",
		}, nil
	}
	if err := ctx.Err(); err != nil {
		return Evaluation{}, err
	}

	signals := []rulesV1Signal{
		{reason: ReasonFanOut, weight: rulesV1FanOutWeight, qualifying: rulesV1FanOut(transfers)},
		{reason: ReasonFanIn, weight: rulesV1FanInWeight, qualifying: rulesV1FanIn(transfers)},
		{reason: ReasonRapidForwarding, weight: rulesV1RapidForwardWeight, qualifying: rulesV1RapidForwarding(transfers)},
		{reason: ReasonTargetConnectedForwarding, weight: rulesV1PathForwardWeight, qualifying: rulesV1TargetConnectedForwarding(input.TargetAddress, transfers)},
		{reason: ReasonCircularFlow, weight: rulesV1CircularFlowWeight, qualifying: rulesV1CircularFlow(transfers)},
	}

	score := 0
	reasons := make([]string, 0, len(signals))
	nodeSignals := make(map[string][]rulesV1Signal)
	for _, signal := range signals {
		if len(signal.qualifying) == 0 {
			continue
		}
		score += signal.weight
		reasons = append(reasons, signal.reason)
		for address := range signal.qualifying {
			nodeSignals[address] = append(nodeSignals[address], signal)
		}
	}
	if score > rulesV1MaximumScore {
		score = rulesV1MaximumScore
	}

	addresses := make([]string, 0, len(nodeSignals))
	for address := range nodeSignals {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	nodes := make([]NodeAssessment, 0, len(addresses))
	for _, address := range addresses {
		nodeScore := 0
		nodeReasons := make([]string, 0, len(nodeSignals[address]))
		for _, signal := range nodeSignals[address] {
			nodeScore += signal.weight
			nodeReasons = append(nodeReasons, signal.reason)
		}
		if nodeScore > rulesV1MaximumScore {
			nodeScore = rulesV1MaximumScore
		}
		nodes = append(nodes, NodeAssessment{
			Address: address,
			Score:   nodeScore,
			Level:   rulesV1Level(nodeScore),
			Reasons: nodeReasons,
		})
	}

	return Evaluation{
		Score:           &score,
		Level:           rulesV1Level(score),
		Reasons:         reasons,
		NodeAssessments: nodes,
		Source:          "rules-v1",
	}, nil
}

func normalizeRulesV1Transfers(collection Collection) ([]rulesV1Transfer, error) {
	transfers := make([]rulesV1Transfer, 0, transferCount(collection))
	seen := make(map[string]struct{})
	for _, transaction := range collection.Transactions {
		for _, transfer := range transaction.Transfers {
			if !unsignedIntegerPattern.MatchString(transfer.AmountSmallestUnit) {
				return nil, errors.New("rules-v1 received an invalid transfer amount")
			}
			amount, ok := new(big.Int).SetString(transfer.AmountSmallestUnit, 10)
			if !ok {
				return nil, errors.New("rules-v1 received an invalid transfer amount")
			}
			key := transferKey(transaction, transfer)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			transfers = append(transfers, rulesV1Transfer{
				key:    key,
				from:   transfer.FromAddress,
				to:     transfer.ToAddress,
				amount: amount,
				at:     transfer.Timestamp,
			})
		}
	}
	sort.Slice(transfers, func(i, j int) bool {
		if !transfers[i].at.Equal(transfers[j].at) {
			return transfers[i].at.Before(transfers[j].at)
		}
		if transfers[i].from != transfers[j].from {
			return transfers[i].from < transfers[j].from
		}
		if transfers[i].to != transfers[j].to {
			return transfers[i].to < transfers[j].to
		}
		return transfers[i].key < transfers[j].key
	})
	return transfers, nil
}

func rulesV1FanOut(transfers []rulesV1Transfer) map[string]struct{} {
	recipients := make(map[string]map[string]struct{})
	for _, transfer := range transfers {
		if transfer.from == "" || transfer.to == "" || transfer.from == transfer.to {
			continue
		}
		if recipients[transfer.from] == nil {
			recipients[transfer.from] = make(map[string]struct{})
		}
		recipients[transfer.from][transfer.to] = struct{}{}
	}
	return addressesMeetingCardinality(recipients, rulesV1FanOutRecipients)
}

func rulesV1FanIn(transfers []rulesV1Transfer) map[string]struct{} {
	senders := make(map[string]map[string]struct{})
	for _, transfer := range transfers {
		if transfer.from == "" || transfer.to == "" || transfer.from == transfer.to {
			continue
		}
		if senders[transfer.to] == nil {
			senders[transfer.to] = make(map[string]struct{})
		}
		senders[transfer.to][transfer.from] = struct{}{}
	}
	return addressesMeetingCardinality(senders, rulesV1FanInSenders)
}

func addressesMeetingCardinality(values map[string]map[string]struct{}, threshold int) map[string]struct{} {
	result := make(map[string]struct{})
	for address, unique := range values {
		if len(unique) >= threshold {
			result[address] = struct{}{}
		}
	}
	return result
}

func rulesV1RapidForwarding(transfers []rulesV1Transfer) map[string]struct{} {
	incoming := make(map[string][]rulesV1Transfer)
	outgoing := make(map[string][]rulesV1Transfer)
	for _, transfer := range transfers {
		incoming[transfer.to] = append(incoming[transfer.to], transfer)
		outgoing[transfer.from] = append(outgoing[transfer.from], transfer)
	}
	result := make(map[string]struct{})
	for address, received := range incoming {
		for _, receipt := range received {
			if receipt.amount.Sign() == 0 {
				continue
			}
			forwarded := new(big.Int)
			windowEnd := receipt.at.Add(rulesV1RapidForwardWindow)
			for _, sent := range outgoing[address] {
				if sent.key == receipt.key || sent.at.Before(receipt.at) || sent.at.After(windowEnd) {
					continue
				}
				forwarded.Add(forwarded, sent.amount)
			}
			if atLeastRatio(forwarded, receipt.amount, rulesV1RapidForwardNumerator, rulesV1RapidForwardDenominator) {
				result[address] = struct{}{}
				break
			}
		}
	}
	return result
}

func rulesV1TargetConnectedForwarding(target string, transfers []rulesV1Transfer) map[string]struct{} {
	result := make(map[string]struct{})
	if target == "" {
		return result
	}
	outgoing := make(map[string][]rulesV1Transfer)
	for _, transfer := range transfers {
		outgoing[transfer.from] = append(outgoing[transfer.from], transfer)
	}
	for _, first := range outgoing[target] {
		if first.to == target || first.amount.Sign() == 0 {
			continue
		}
		windowEnd := first.at.Add(rulesV1PathForwardWindow)
		for _, second := range outgoing[first.to] {
			if second.key == first.key || second.to == first.to || second.at.Before(first.at) || second.at.After(windowEnd) {
				continue
			}
			if atLeastRatio(second.amount, first.amount, rulesV1PathForwardNumerator, rulesV1PathForwardDenominator) {
				result[target] = struct{}{}
				result[first.to] = struct{}{}
				result[second.to] = struct{}{}
			}
		}
	}
	return result
}

func rulesV1CircularFlow(transfers []rulesV1Transfer) map[string]struct{} {
	adjacencySets := make(map[string]map[string]struct{})
	for _, transfer := range transfers {
		if transfer.from == "" || transfer.to == "" || transfer.from == transfer.to {
			continue
		}
		if adjacencySets[transfer.from] == nil {
			adjacencySets[transfer.from] = make(map[string]struct{})
		}
		adjacencySets[transfer.from][transfer.to] = struct{}{}
	}
	adjacency := make(map[string][]string, len(adjacencySets))
	starts := make([]string, 0, len(adjacencySets))
	for address, peers := range adjacencySets {
		adjacency[address] = sortedSet(peers)
		starts = append(starts, address)
	}
	sort.Strings(starts)
	result := make(map[string]struct{})
	for _, start := range starts {
		rulesV1FindCycles(start, adjacency, []string{start}, result)
	}
	return result
}

func rulesV1FindCycles(current string, adjacency map[string][]string, path []string, result map[string]struct{}) {
	for _, next := range adjacency[current] {
		cycleStart := indexOf(path, next)
		if cycleStart >= 0 {
			hops := len(path) - cycleStart
			if hops >= rulesV1MinimumCycleHops && hops <= rulesV1MaximumCycleHops {
				for _, address := range path[cycleStart:] {
					result[address] = struct{}{}
				}
			}
			continue
		}
		if len(path) >= rulesV1MaximumCycleHops {
			continue
		}
		rulesV1FindCycles(next, adjacency, append(path, next), result)
	}
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func atLeastRatio(numerator, denominator *big.Int, thresholdNumerator, thresholdDenominator int64) bool {
	if denominator.Sign() <= 0 {
		return false
	}
	left := new(big.Int).Mul(new(big.Int).Set(numerator), big.NewInt(thresholdDenominator))
	right := new(big.Int).Mul(new(big.Int).Set(denominator), big.NewInt(thresholdNumerator))
	return left.Cmp(right) >= 0
}

func rulesV1Level(score int) string {
	switch {
	case score >= 75:
		return "critical"
	case score >= 50:
		return "high"
	case score >= 25:
		return "medium"
	default:
		return "low"
	}
}
