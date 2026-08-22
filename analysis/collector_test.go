package analysis_test

import (
	"chaintrace/analysis"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

const (
	collectorTarget = "target"
	collectorUSDT   = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
)

type scriptedPage struct {
	page analysis.AddressTransferPage
	err  error
}

type scriptedPageProvider struct {
	pages map[string]scriptedPage
	calls []string
}

func (p *scriptedPageProvider) CaptureConfirmedCutoff(context.Context, string) (analysis.BlockCutoff, error) {
	return analysis.BlockCutoff{}, nil
}

func (p *scriptedPageProvider) FetchAddressTransfers(_ context.Context, request analysis.AddressTransferPageRequest) (analysis.AddressTransferPage, error) {
	key := request.Address + "|" + request.Cursor
	p.calls = append(p.calls, key)
	result := p.pages[key]
	return result.page, result.err
}

func TestCollectorUsesStableBFSAndDeduplicatesLoopsOverlapsAndTransferEvents(t *testing.T) {
	cutoff := collectorCutoff()
	tx1 := collectorTransaction("tx-1", cutoff.Add(-4*time.Hour),
		collectorTransfer("event-2", collectorTarget, "charlie", "200", cutoff.Add(-4*time.Hour)),
		collectorTransfer("event-1", collectorTarget, "bravo", "100", cutoff.Add(-4*time.Hour)),
	)
	tx2 := collectorTransaction("tx-2", cutoff.Add(-3*time.Hour),
		collectorTransfer("event-1", "bravo", collectorTarget, "50", cutoff.Add(-3*time.Hour)),
		collectorTransfer("event-2", "bravo", "delta", "40", cutoff.Add(-3*time.Hour)),
	)
	tx3 := collectorTransaction("tx-3", cutoff.Add(-2*time.Hour),
		collectorTransfer("event-1", "charlie", "delta", "30", cutoff.Add(-2*time.Hour)),
	)
	provider := &scriptedPageProvider{pages: map[string]scriptedPage{
		"target|":     {page: analysis.AddressTransferPage{Transactions: []analysis.BlockchainTransaction{tx1}, NextCursor: "next"}},
		"target|next": {page: analysis.AddressTransferPage{Transactions: []analysis.BlockchainTransaction{tx1}}},
		"bravo|":      {page: analysis.AddressTransferPage{Transactions: []analysis.BlockchainTransaction{tx1, tx2}}},
		"charlie|":    {page: analysis.AddressTransferPage{Transactions: []analysis.BlockchainTransaction{tx3, tx1}}},
	}}

	collection, err := analysis.NewCollector(provider).Collect(context.Background(), collectorRequest(cutoff, 20, 2))
	if err != nil {
		t.Fatalf("collect BFS fixture: %v", err)
	}
	if got, want := provider.calls, []string{"target|", "target|next", "bravo|", "charlie|"}; !reflect.DeepEqual(got, want) {
		t.Errorf("provider calls = %#v, want %#v", got, want)
	}
	if got := collectionEventIDs(collection); !reflect.DeepEqual(got, []string{"tx-1:event-1", "tx-1:event-2", "tx-2:event-1", "tx-2:event-2", "tx-3:event-1"}) {
		t.Errorf("collected events = %#v", got)
	}
	if collection.StopReason != analysis.StopReasonTraversalDepthReached || collection.Partial || collection.Confidence != 100 || collection.ReachedDepth != 2 {
		t.Errorf("bounded BFS coverage = %#v", collection)
	}
}

func TestCollectorCountsUniqueEligibleTransfersTowardLimit(t *testing.T) {
	cutoff := collectorCutoff()
	duplicate := collectorTransfer("event-1", collectorTarget, "bravo", "10", cutoff.Add(-time.Hour))
	provider := &scriptedPageProvider{pages: map[string]scriptedPage{
		"target|": {page: analysis.AddressTransferPage{Transactions: []analysis.BlockchainTransaction{
			collectorTransaction("tx-2", cutoff.Add(-time.Hour), collectorTransfer("event-2", collectorTarget, "charlie", "20", cutoff.Add(-time.Hour))),
			collectorTransaction("tx-1", cutoff.Add(-time.Hour), duplicate, duplicate),
			collectorTransaction("tx-3", cutoff.Add(-time.Hour), collectorTransfer("event-3", collectorTarget, "delta", "30", cutoff.Add(-time.Hour))),
		}}},
	}}

	collection, err := analysis.NewCollector(provider).Collect(context.Background(), collectorRequest(cutoff, 2, 4))
	if err != nil {
		t.Fatalf("collect transfer limit: %v", err)
	}
	if got := collectionEventIDs(collection); !reflect.DeepEqual(got, []string{"tx-1:event-1", "tx-2:event-2"}) {
		t.Errorf("limit events = %#v", got)
	}
	if collection.StopReason != analysis.StopReasonTransferLimitReached || collection.ReachedDepth != 1 || collection.Partial || collection.Confidence != 100 {
		t.Errorf("limit coverage = %#v", collection)
	}
}

func TestCollectorFiltersIneligibleTransfers(t *testing.T) {
	cutoff := collectorCutoff()
	valid := collectorTransaction("valid", cutoff.Add(-time.Hour), collectorTransfer("eligible", collectorTarget, collectorTarget, "900719925474099312345678", cutoff.Add(-time.Hour)))
	wrongNetwork := collectorTransaction("wrong-network", cutoff.Add(-time.Hour), collectorTransfer("event", collectorTarget, collectorTarget, "1", cutoff.Add(-time.Hour)))
	wrongNetwork.Network = "OTHER"
	failed := collectorTransaction("failed", cutoff.Add(-time.Hour), collectorTransfer("event", collectorTarget, collectorTarget, "1", cutoff.Add(-time.Hour)))
	failed.Successful = false
	unconfirmed := collectorTransaction("unconfirmed", cutoff.Add(-time.Hour), collectorTransfer("event", collectorTarget, collectorTarget, "1", cutoff.Add(-time.Hour)))
	unconfirmed.Confirmed = false
	outOfWindow := collectorTransaction("old", cutoff.Add(-31*24*time.Hour), collectorTransfer("event", collectorTarget, collectorTarget, "1", cutoff.Add(-31*24*time.Hour)))
	wrongContract := collectorTransaction("wrong-contract", cutoff.Add(-time.Hour), collectorTransfer("event", collectorTarget, collectorTarget, "1", cutoff.Add(-time.Hour)))
	wrongContract.Transfers[0].ContractAddress = "not-usdt"
	malformedAmount := collectorTransaction("bad-amount", cutoff.Add(-time.Hour), collectorTransfer("event", collectorTarget, collectorTarget, "1.5", cutoff.Add(-time.Hour)))
	provider := &scriptedPageProvider{pages: map[string]scriptedPage{
		"target|": {page: analysis.AddressTransferPage{Transactions: []analysis.BlockchainTransaction{
			wrongNetwork, failed, unconfirmed, outOfWindow, wrongContract, malformedAmount, valid,
		}}},
	}}

	collection, err := analysis.NewCollector(provider).Collect(context.Background(), collectorRequest(cutoff, 10, 2))
	if err != nil {
		t.Fatalf("collect eligible evidence: %v", err)
	}
	if got := collectionEventIDs(collection); !reflect.DeepEqual(got, []string{"valid:eligible"}) {
		t.Errorf("eligible events = %#v", got)
	}
	if collection.StopReason != analysis.StopReasonSourceExhausted || collection.Confidence != 100 || collection.ReachedDepth != 0 {
		t.Errorf("eligible source coverage = %#v", collection)
	}
}

func TestCollectorPublishesTypedInterruptionsAsPartialOnlyWithEvidence(t *testing.T) {
	cutoff := collectorCutoff()
	for _, interruption := range []struct {
		kind analysis.InterruptionKind
		want string
	}{
		{analysis.InterruptionRateLimited, analysis.StopReasonProviderRateLimited},
		{analysis.InterruptionUnavailable, analysis.StopReasonProviderUnavailable},
		{analysis.InterruptionResourceLimit, analysis.StopReasonResourceLimitReached},
	} {
		t.Run(string(interruption.kind), func(t *testing.T) {
			provider := &scriptedPageProvider{pages: map[string]scriptedPage{
				"target|": {
					page: analysis.AddressTransferPage{
						Transactions: []analysis.BlockchainTransaction{collectorTransaction("tx", cutoff.Add(-time.Hour), collectorTransfer("event", collectorTarget, "peer", "1", cutoff.Add(-time.Hour)))},
						NextCursor:   "next",
					},
				},
				"target|next": {err: &analysis.CollectionInterruption{Kind: interruption.kind, Err: errors.New("fixture interruption")}},
			}}

			collection, err := analysis.NewCollector(provider).Collect(context.Background(), collectorRequest(cutoff, 10, 2))
			if err != nil {
				t.Fatalf("collect partial evidence: %v", err)
			}
			if !collection.Partial || collection.StopReason != interruption.want || collection.Confidence != 10 || len(collectionEventIDs(collection)) != 1 {
				t.Errorf("partial collection = %#v", collection)
			}
		})
	}
}

func TestCollectorDistinguishesNoEligibleTransfersFromInterruptedEmptyCollection(t *testing.T) {
	cutoff := collectorCutoff()
	wrongContract := collectorTransaction("wrong", cutoff.Add(-time.Hour), collectorTransfer("event", collectorTarget, "peer", "1", cutoff.Add(-time.Hour)))
	wrongContract.Transfers[0].ContractAddress = "not-usdt"
	exhausted := &scriptedPageProvider{pages: map[string]scriptedPage{
		"target|": {page: analysis.AddressTransferPage{Transactions: []analysis.BlockchainTransaction{wrongContract}}},
	}}

	collection, err := analysis.NewCollector(exhausted).Collect(context.Background(), collectorRequest(cutoff, 10, 2))
	if err != nil {
		t.Fatalf("collect empty exhausted source: %v", err)
	}
	if collection.StopReason != analysis.StopReasonNoEligibleTransfers || collection.Partial || collection.Confidence != 0 || collection.ReachedDepth != 0 || len(collection.Transactions) != 0 {
		t.Errorf("empty exhausted collection = %#v", collection)
	}

	interrupted := &scriptedPageProvider{pages: map[string]scriptedPage{
		"target|": {err: &analysis.CollectionInterruption{Kind: analysis.InterruptionUnavailable, Err: errors.New("down")}},
	}}
	collection, err = analysis.NewCollector(interrupted).Collect(context.Background(), collectorRequest(cutoff, 10, 2))
	if err == nil || collection.StopReason != "" || len(collection.Transactions) != 0 {
		t.Fatalf("empty interruption = (%#v, %v), want failed collection", collection, err)
	}
}

func collectorCutoff() time.Time {
	return time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
}

func collectorRequest(cutoff time.Time, limit, depth int) analysis.CollectionRequest {
	return analysis.CollectionRequest{
		TargetAddress:  collectorTarget,
		Network:        "TRON_MAINNET",
		Asset:          analysis.AssetUSDT,
		WindowStart:    cutoff.Add(-30 * 24 * time.Hour),
		WindowEnd:      cutoff,
		CutoffBlockID:  "cutoff",
		TransferLimit:  limit,
		TraversalDepth: depth,
	}
}

func collectorTransaction(hash string, timestamp time.Time, transfers ...analysis.TRC20Transfer) analysis.BlockchainTransaction {
	return analysis.BlockchainTransaction{
		Network:        "TRON_MAINNET",
		Hash:           hash,
		BlockID:        "block-" + hash,
		BlockTimestamp: timestamp,
		Successful:     true,
		Confirmed:      true,
		Transfers:      transfers,
	}
}

func collectorTransfer(event, from, to, amount string, timestamp time.Time) analysis.TRC20Transfer {
	return analysis.TRC20Transfer{
		EventIdentity:      event,
		ContractAddress:    collectorUSDT,
		Asset:              analysis.AssetUSDT,
		FromAddress:        from,
		ToAddress:          to,
		AmountSmallestUnit: amount,
		Decimals:           6,
		Timestamp:          timestamp,
	}
}

func collectionEventIDs(collection analysis.Collection) []string {
	ids := make([]string, 0)
	for _, transaction := range collection.Transactions {
		for _, transfer := range transaction.Transfers {
			ids = append(ids, fmt.Sprintf("%s:%s", transaction.Hash, transfer.EventIdentity))
		}
	}
	return ids
}

// A busy address can page indefinitely while admitting nothing eligible, so neither
// the Transfer Limit nor the per-address page cap ends the run. The budget must.
func TestCollectorBoundsProviderCallsOnEndlessPaging(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		page := atomic.AddInt32(&calls, 1)
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(response, `{"data":[],"success":true,"meta":{"page_size":0,"fingerprint":"fp-%d"}}`, page)
	}))
	defer server.Close()
	// Headroom on the per-address page cap so the run's call budget is the only
	// bound left; in production the frontier spreads across addresses and resets
	// that cap anyway.
	config := recordedTronGridConfig(server.URL)
	config.MaxPages = 10000
	provider, err := analysis.NewTronGridProvider(config)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)

	_, err = analysis.NewCollector(provider).Collect(context.Background(), analysis.CollectionRequest{
		TargetAddress:  recordedTarget,
		Network:        "TRON_MAINNET",
		Asset:          analysis.AssetUSDT,
		WindowStart:    cutoff.Add(-30 * 24 * time.Hour),
		WindowEnd:      cutoff,
		CutoffBlockID:  "0000000003abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TransferLimit:  10,
		TraversalDepth: 2,
	})

	assertInterruptionKind(t, err, analysis.InterruptionResourceLimit)
	if want := int32(200 + 6*10); atomic.LoadInt32(&calls) != want {
		t.Errorf("provider calls = %d, want the budget %d", atomic.LoadInt32(&calls), want)
	}
}
