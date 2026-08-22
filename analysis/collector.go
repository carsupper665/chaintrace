package analysis

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

type AddressTransferPageRequest struct {
	Address        string
	Network        string
	Asset          string
	WindowStart    time.Time
	WindowEnd      time.Time
	CutoffBlockID  string
	TransferLimit  int
	TraversalDepth int
	Cursor         string
}

type AddressTransferPage struct {
	Transactions []BlockchainTransaction
	NextCursor   string
}

type InterruptionKind string

const (
	InterruptionRateLimited   InterruptionKind = "rate_limited"
	InterruptionUnavailable   InterruptionKind = "unavailable"
	InterruptionResourceLimit InterruptionKind = "resource_limit"
)

type CollectionInterruption struct {
	Kind InterruptionKind
	Err  error
}

func (e *CollectionInterruption) Error() string {
	if e == nil {
		return "collection interrupted"
	}
	if e.Err != nil {
		return fmt.Sprintf("collection %s: %v", e.Kind, e.Err)
	}
	return "collection " + string(e.Kind)
}

func (e *CollectionInterruption) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Each provider page costs one list call plus one receipt call per transaction on it,
// and the frontier grows with every admitted transfer, so neither the per-address page
// cap nor the Transfer Limit bounds a run's call volume. The budget below does.
// Exhausting it stops collection as a resource limit, which publishes Partial coverage
// rather than silently truncating the evidence. One run collects on one goroutine, so
// the counter needs no synchronisation.
type callBudgetKey struct{}

func withCallBudget(ctx context.Context, transferLimit int) context.Context {
	remaining := 200 + 6*transferLimit
	return context.WithValue(ctx, callBudgetKey{}, &remaining)
}

func spendProviderCall(ctx context.Context) error {
	remaining, _ := ctx.Value(callBudgetKey{}).(*int)
	if remaining == nil {
		return nil
	}
	if *remaining--; *remaining < 0 {
		return errors.New("provider call budget exhausted")
	}
	return nil
}

type Collector struct {
	provider ChainDataProvider
}

func NewCollector(provider ChainDataProvider) *Collector {
	return &Collector{provider: provider}
}

func (c *Collector) Collect(ctx context.Context, request CollectionRequest) (Collection, error) {
	if c.provider == nil {
		return Collection{}, &CollectionInterruption{Kind: InterruptionUnavailable, Err: errors.New("chain data provider is not configured")}
	}
	if request.TargetAddress == "" || request.Network != "TRON_MAINNET" || request.Asset != AssetUSDT || request.WindowStart.IsZero() || request.WindowEnd.IsZero() || request.WindowStart.After(request.WindowEnd) ||
		request.TransferLimit < 1 || request.TransferLimit > MaximumTransferLimit || request.TraversalDepth < 1 || request.TraversalDepth > MaximumTraversalDepth {
		return Collection{}, errors.New("invalid collection request")
	}

	ctx = withCallBudget(ctx, request.TransferLimit)

	transactions := make(map[string]*BlockchainTransaction)
	seenTransfers := make(map[string]struct{})
	visitedAddresses := map[string]struct{}{request.TargetAddress: {}}
	frontier := []string{request.TargetAddress}
	reachedDepth := 0

	for depth := 0; depth < request.TraversalDepth && len(frontier) > 0; depth++ {
		sort.Strings(frontier)
		nextAddresses := make(map[string]struct{})
		for _, address := range frontier {
			cursor := ""
			seenCursors := map[string]struct{}{"": {}}
			for {
				page, err := c.provider.FetchAddressTransfers(ctx, AddressTransferPageRequest{
					Address:        address,
					Network:        request.Network,
					Asset:          request.Asset,
					WindowStart:    request.WindowStart,
					WindowEnd:      request.WindowEnd,
					CutoffBlockID:  request.CutoffBlockID,
					TransferLimit:  request.TransferLimit,
					TraversalDepth: request.TraversalDepth,
					Cursor:         cursor,
				})
				if err != nil {
					return interruptedCollection(request, transactions, reachedDepth, err)
				}

				for _, candidate := range eligiblePageTransfers(page, request, address) {
					key := transferKey(candidate.transaction, candidate.transfer)
					if _, exists := seenTransfers[key]; exists {
						continue
					}
					seenTransfers[key] = struct{}{}
					admitTransfer(transactions, candidate.transaction, candidate.transfer)

					for _, peer := range transferPeers(address, candidate.transfer) {
						if _, visited := visitedAddresses[peer]; visited {
							continue
						}
						visitedAddresses[peer] = struct{}{}
						nextAddresses[peer] = struct{}{}
						if depth+1 > reachedDepth {
							reachedDepth = depth + 1
						}
					}

					if len(seenTransfers) == request.TransferLimit {
						return completedCollection(transactions, reachedDepth, StopReasonTransferLimitReached, false, 100), nil
					}
				}

				if page.NextCursor == "" {
					break
				}
				if _, repeated := seenCursors[page.NextCursor]; repeated {
					interruption := &CollectionInterruption{Kind: InterruptionResourceLimit, Err: errors.New("provider repeated a page cursor")}
					return interruptedCollection(request, transactions, reachedDepth, interruption)
				}
				seenCursors[page.NextCursor] = struct{}{}
				cursor = page.NextCursor
			}
		}

		if len(nextAddresses) == 0 {
			if len(seenTransfers) == 0 {
				return completedCollection(transactions, 0, StopReasonNoEligibleTransfers, false, 0), nil
			}
			return completedCollection(transactions, reachedDepth, StopReasonSourceExhausted, false, 100), nil
		}
		if depth+1 == request.TraversalDepth {
			return completedCollection(transactions, reachedDepth, StopReasonTraversalDepthReached, false, 100), nil
		}
		frontier = sortedSet(nextAddresses)
	}

	if len(seenTransfers) == 0 {
		return completedCollection(transactions, 0, StopReasonNoEligibleTransfers, false, 0), nil
	}
	return completedCollection(transactions, reachedDepth, StopReasonSourceExhausted, false, 100), nil
}

type pageTransfer struct {
	transaction BlockchainTransaction
	transfer    TRC20Transfer
}

func eligiblePageTransfers(page AddressTransferPage, request CollectionRequest, address string) []pageTransfer {
	eligible := make([]pageTransfer, 0)
	for _, transaction := range page.Transactions {
		if !eligibleTransaction(transaction, request) {
			continue
		}
		for _, transfer := range transaction.Transfers {
			if eligibleTransfer(transfer, request, address) {
				eligible = append(eligible, pageTransfer{transaction: transaction, transfer: transfer})
			}
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		left := transferKey(eligible[i].transaction, eligible[i].transfer)
		right := transferKey(eligible[j].transaction, eligible[j].transfer)
		return left < right
	})
	return eligible
}

func eligibleTransaction(transaction BlockchainTransaction, request CollectionRequest) bool {
	return transaction.Network == request.Network && transaction.Hash != "" && transaction.BlockID != "" &&
		transaction.Successful && transaction.Confirmed && !transaction.BlockTimestamp.IsZero() &&
		!transaction.BlockTimestamp.Before(request.WindowStart) && !transaction.BlockTimestamp.After(request.WindowEnd)
}

func eligibleTransfer(transfer TRC20Transfer, request CollectionRequest, address string) bool {
	return transfer.EventIdentity != "" && transfer.ContractAddress == TRONMainnetUSDTContract && transfer.Asset == request.Asset &&
		transfer.FromAddress != "" && transfer.ToAddress != "" && (transfer.FromAddress == address || transfer.ToAddress == address) &&
		transfer.Decimals == 6 && !transfer.Timestamp.IsZero() && !transfer.Timestamp.Before(request.WindowStart) && !transfer.Timestamp.After(request.WindowEnd) &&
		unsignedIntegerPattern.MatchString(transfer.AmountSmallestUnit)
}

func transferKey(transaction BlockchainTransaction, transfer TRC20Transfer) string {
	return transaction.Network + "\x00" + transaction.Hash + "\x00" + transfer.EventIdentity
}

func transferPeers(address string, transfer TRC20Transfer) []string {
	peers := make([]string, 0, 2)
	if transfer.FromAddress != address {
		peers = append(peers, transfer.FromAddress)
	}
	if transfer.ToAddress != address && transfer.ToAddress != transfer.FromAddress {
		peers = append(peers, transfer.ToAddress)
	}
	return peers
}

func admitTransfer(transactions map[string]*BlockchainTransaction, source BlockchainTransaction, transfer TRC20Transfer) {
	key := source.Network + "\x00" + source.Hash
	transaction := transactions[key]
	if transaction == nil {
		copy := source
		copy.Transfers = nil
		transaction = &copy
		transactions[key] = transaction
	}
	transaction.Transfers = append(transaction.Transfers, transfer)
}

func interruptedCollection(request CollectionRequest, transactions map[string]*BlockchainTransaction, reachedDepth int, err error) (Collection, error) {
	var interruption *CollectionInterruption
	if !errors.As(err, &interruption) || transferMapCount(transactions) == 0 {
		return Collection{}, err
	}
	stopReason := interruptionStopReason(interruption.Kind)
	if stopReason == "" {
		return Collection{}, err
	}
	collected := transferMapCount(transactions)
	confidence := 100 * collected / request.TransferLimit
	if confidence > 75 {
		confidence = 75
	}
	return completedCollection(transactions, reachedDepth, stopReason, true, confidence), nil
}

func interruptionStopReason(kind InterruptionKind) string {
	switch kind {
	case InterruptionRateLimited:
		return StopReasonProviderRateLimited
	case InterruptionUnavailable:
		return StopReasonProviderUnavailable
	case InterruptionResourceLimit:
		return StopReasonResourceLimitReached
	default:
		return ""
	}
}

func completedCollection(transactions map[string]*BlockchainTransaction, reachedDepth int, stopReason string, partial bool, confidence int) Collection {
	keys := make([]string, 0, len(transactions))
	for key := range transactions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := Collection{
		Transactions: make([]BlockchainTransaction, 0, len(keys)),
		StopReason:   stopReason,
		Partial:      partial,
		Confidence:   confidence,
		ReachedDepth: reachedDepth,
	}
	for _, key := range keys {
		transaction := *transactions[key]
		sort.Slice(transaction.Transfers, func(i, j int) bool {
			return transaction.Transfers[i].EventIdentity < transaction.Transfers[j].EventIdentity
		})
		result.Transactions = append(result.Transactions, transaction)
	}
	return result
}

func transferMapCount(transactions map[string]*BlockchainTransaction) int {
	count := 0
	for _, transaction := range transactions {
		count += len(transaction.Transfers)
	}
	return count
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
