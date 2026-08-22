package analysis_test

import (
	"chaintrace/analysis"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	recordedTarget = "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"
	recordedPeer   = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	recordedTx     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	recordedNewTx  = "9999999999999999999999999999999999999999999999999999999999999999"
)

func TestTronGridCapturesLatestConfirmedBlock(t *testing.T) {
	fixture := tronGridFixture(t, "cutoff.json")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/walletsolidity/getnowblock" {
			t.Errorf("cutoff request = %s %s", request.Method, request.URL.Path)
			http.NotFound(response, request)
			return
		}
		if got := request.Header.Get("TRON-PRO-API-KEY"); got != "recorded-fixture-key" {
			t.Errorf("API key header = %q", got)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(fixture)
	}))
	t.Cleanup(server.Close)
	provider := newRecordedTronGridProvider(t, server.URL)

	cutoff, err := provider.CaptureConfirmedCutoff(context.Background(), "TRON_MAINNET")
	if err != nil {
		t.Fatalf("capture confirmed cutoff: %v", err)
	}
	if cutoff.BlockID != "0000000003abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Errorf("block ID = %q", cutoff.BlockID)
	}
	wantTimestamp := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	if !cutoff.Timestamp.Equal(wantTimestamp) {
		t.Errorf("timestamp = %s, want %s", cutoff.Timestamp, wantTimestamp)
	}
}

func TestTronGridNormalizesFingerprintPagesUsingVerifiedReceiptLogIndexes(t *testing.T) {
	server := newRecordedTransferServer(t)
	defer server.Close()
	provider := newRecordedTronGridProvider(t, server.URL)
	request := recordedTransferRequest()

	first, err := provider.FetchAddressTransfers(context.Background(), request)
	if err != nil {
		t.Fatalf("fetch first recorded page: %v", err)
	}
	if first.NextCursor == "" || first.NextCursor == "recorded-page-2" {
		t.Fatalf("provider-neutral next cursor = %q", first.NextCursor)
	}
	assertRecordedTransactions(t, first.Transactions, []recordedTransferExpectation{
		{recordedTx, "61603000", "log:1", recordedTarget, recordedPeer, "900719925474099312345678"},
		{recordedTx, "61603000", "log:3", recordedPeer, recordedTarget, "1"},
	})

	request.Cursor = first.NextCursor
	second, err := provider.FetchAddressTransfers(context.Background(), request)
	if err != nil {
		t.Fatalf("fetch second recorded page: %v", err)
	}
	if second.NextCursor != "" {
		t.Errorf("final next cursor = %q", second.NextCursor)
	}
	assertRecordedTransactions(t, second.Transactions, []recordedTransferExpectation{
		{recordedNewTx, "61603500", "log:0", recordedTarget, recordedPeer, "42"},
		{recordedTx, "61603000", "log:1", recordedTarget, recordedPeer, "900719925474099312345678"},
	})
}

func TestTronGridPagesDeduplicateOverlappingEventsThroughCollector(t *testing.T) {
	server := newRecordedTransferServer(t)
	defer server.Close()
	provider := newRecordedTronGridProvider(t, server.URL)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)

	collection, err := analysis.NewCollector(provider).Collect(context.Background(), analysis.CollectionRequest{
		TargetAddress:  recordedTarget,
		Network:        "TRON_MAINNET",
		Asset:          analysis.AssetUSDT,
		WindowStart:    cutoff.Add(-30 * 24 * time.Hour),
		WindowEnd:      cutoff,
		CutoffBlockID:  "0000000003abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TransferLimit:  10,
		TraversalDepth: 1,
	})
	if err != nil {
		t.Fatalf("collect overlapping recorded pages: %v", err)
	}
	var identities []string
	for _, transaction := range collection.Transactions {
		for _, transfer := range transaction.Transfers {
			identities = append(identities, transaction.Hash+":"+transfer.EventIdentity)
		}
	}
	if want := []string{recordedNewTx + ":log:0", recordedTx + ":log:1", recordedTx + ":log:3"}; !reflect.DeepEqual(identities, want) {
		t.Errorf("collected event identities = %#v, want %#v", identities, want)
	}
}

func TestTronGridRetriesBoundedTransientResponses(t *testing.T) {
	fixture := tronGridFixture(t, "cutoff.json")
	rateLimitFixture := tronGridFixture(t, "rate-limit.json")
	tests := []struct {
		name       string
		status     int
		retryAfter string
		recover    bool
		wantKind   analysis.InterruptionKind
		wantCalls  int
	}{
		{name: "429 honors bounded Retry-After", status: http.StatusTooManyRequests, retryAfter: "1", recover: true},
		{name: "429 without Retry-After exhausts as rate limit", status: http.StatusTooManyRequests, wantKind: analysis.InterruptionRateLimited},
		{name: "403 fails fast as unavailable", status: http.StatusForbidden, wantKind: analysis.InterruptionUnavailable, wantCalls: 1},
		{name: "500 retries and recovers", status: http.StatusServiceUnavailable, recover: true},
		{name: "500 exhausts as unavailable", status: http.StatusServiceUnavailable, wantKind: analysis.InterruptionUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				call := calls.Add(1)
				if call == 1 || !test.recover {
					if test.retryAfter != "" {
						response.Header().Set("Retry-After", test.retryAfter)
					}
					response.WriteHeader(test.status)
					_, _ = response.Write(rateLimitFixture)
					return
				}
				_, _ = response.Write(fixture)
			}))
			defer server.Close()
			config := recordedTronGridConfig(server.URL)
			if test.retryAfter != "" {
				config.MaxRetryDelay = 20 * time.Millisecond
			}
			provider, err := analysis.NewTronGridProvider(config)
			if err != nil {
				t.Fatalf("create retry provider: %v", err)
			}

			started := time.Now()
			_, err = provider.CaptureConfirmedCutoff(context.Background(), "TRON_MAINNET")
			if test.wantKind == "" {
				if err != nil {
					t.Fatalf("retrying cutoff: %v", err)
				}
			} else {
				assertInterruptionKind(t, err, test.wantKind)
			}
			wantCalls := test.wantCalls
			if wantCalls == 0 {
				wantCalls = 2
			}
			if calls.Load() != int32(wantCalls) {
				t.Errorf("HTTP calls = %d, want %d", calls.Load(), wantCalls)
			}
			if test.retryAfter != "" && time.Since(started) < 15*time.Millisecond {
				t.Errorf("Retry-After was not applied within configured bound")
			}
		})
	}
}

func TestTronGridMapsTimeoutAfterBoundedRetriesToUnavailable(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		<-release
	}))
	defer server.Close()
	config := recordedTronGridConfig(server.URL)
	config.HTTPTimeout = 10 * time.Millisecond
	provider, err := analysis.NewTronGridProvider(config)
	if err != nil {
		t.Fatalf("create timeout provider: %v", err)
	}

	_, err = provider.CaptureConfirmedCutoff(context.Background(), "TRON_MAINNET")
	close(release)
	assertInterruptionKind(t, err, analysis.InterruptionUnavailable)
	if calls.Load() != 2 {
		t.Errorf("timeout HTTP calls = %d, want 2", calls.Load())
	}
}

func TestTronGridStopsRetryingWhenCancelled(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		close(started)
		<-release
	}))
	defer server.Close()
	provider := newRecordedTronGridProvider(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := provider.CaptureConfirmedCutoff(ctx, "TRON_MAINNET")
		result <- err
	}()
	<-started
	cancel()

	err := <-result
	close(release)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled cutoff error = %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("cancelled HTTP calls = %d, want no retry", calls.Load())
	}
}

func TestTronGridRejectsMalformedAndOversizedResponses(t *testing.T) {
	for _, test := range []struct {
		name     string
		body     []byte
		maxBytes int64
		wantKind analysis.InterruptionKind
	}{
		{name: "malformed JSON", body: tronGridFixture(t, "malformed.json"), maxBytes: 1024, wantKind: analysis.InterruptionUnavailable},
		{name: "response size", body: []byte(strings.Repeat("x", 65)), maxBytes: 64, wantKind: analysis.InterruptionResourceLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write(test.body)
			}))
			defer server.Close()
			config := recordedTronGridConfig(server.URL)
			config.MaxResponseBytes = test.maxBytes
			provider, err := analysis.NewTronGridProvider(config)
			if err != nil {
				t.Fatalf("create guarded provider: %v", err)
			}

			_, err = provider.CaptureConfirmedCutoff(context.Background(), "TRON_MAINNET")
			assertInterruptionKind(t, err, test.wantKind)
		})
	}
}

func TestTronGridEnforcesConfiguredPageLimit(t *testing.T) {
	server := newRecordedTransferServer(t)
	defer server.Close()
	config := recordedTronGridConfig(server.URL)
	config.MaxPages = 1
	provider, err := analysis.NewTronGridProvider(config)
	if err != nil {
		t.Fatalf("create page-bounded provider: %v", err)
	}
	request := recordedTransferRequest()
	first, err := provider.FetchAddressTransfers(context.Background(), request)
	if err != nil {
		t.Fatalf("fetch bounded first page: %v", err)
	}
	request.Cursor = first.NextCursor

	_, err = provider.FetchAddressTransfers(context.Background(), request)
	assertInterruptionKind(t, err, analysis.InterruptionResourceLimit)
}

type recordedTransferExpectation struct {
	hash, blockID, event, from, to, amount string
}

func assertRecordedTransactions(t *testing.T, transactions []analysis.BlockchainTransaction, want []recordedTransferExpectation) {
	t.Helper()
	var got []recordedTransferExpectation
	for _, transaction := range transactions {
		if transaction.Network != "TRON_MAINNET" || !transaction.Successful || !transaction.Confirmed || transaction.BlockTimestamp.IsZero() {
			t.Errorf("transaction provenance = %#v", transaction)
		}
		for _, transfer := range transaction.Transfers {
			if transfer.ContractAddress != analysis.TRONMainnetUSDTContract || transfer.Asset != analysis.AssetUSDT || transfer.Decimals != 6 || transfer.Timestamp.IsZero() {
				t.Errorf("transfer metadata = %#v", transfer)
			}
			got = append(got, recordedTransferExpectation{transaction.Hash, transaction.BlockID, transfer.EventIdentity, transfer.FromAddress, transfer.ToAddress, transfer.AmountSmallestUnit})
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("normalized transfers = %#v, want %#v", got, want)
	}
}

func newRecordedTransferServer(t *testing.T) *httptest.Server {
	t.Helper()
	fixtures := map[string][]byte{
		"page-1":                tronGridFixture(t, "trc20-page-1.json"),
		"page-2":                tronGridFixture(t, "trc20-page-2.json"),
		recordedTx:              tronGridFixture(t, "transaction-info-multi.json"),
		recordedNewTx:           tronGridFixture(t, "transaction-info-new.json"),
		strings.Repeat("b", 64): tronGridFixture(t, "transaction-info-failed.json"),
		strings.Repeat("c", 64): tronGridFixture(t, "transaction-info-pending.json"),
	}
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("TRON-PRO-API-KEY") != "recorded-fixture-key" {
			t.Errorf("missing API key for %s", request.URL.Path)
		}
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/accounts/"+recordedTarget+"/transactions/trc20":
			query := request.URL.Query()
			limit, _ := strconv.Atoi(query.Get("limit"))
			if query.Get("only_confirmed") != "true" || query.Get("contract_address") != analysis.TRONMainnetUSDTContract || limit < 1 || limit > 200 ||
				query.Get("min_timestamp") != "1783252800000" || query.Get("max_timestamp") != "1785844800000" || query.Has("only_from") || query.Has("only_to") {
				t.Errorf("TRC20 query = %s", request.URL.RawQuery)
			}
			name := "page-1"
			if query.Get("fingerprint") != "" {
				if query.Get("fingerprint") != "recorded-page-2" {
					t.Errorf("fingerprint = %q", query.Get("fingerprint"))
				}
				name = "page-2"
			}
			_, _ = response.Write(fixtures[name])
		case request.Method == http.MethodPost && request.URL.Path == "/walletsolidity/gettransactioninfobyid":
			var body struct {
				Value string `json:"value"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode transaction info request: %v", err)
			}
			fixture := fixtures[body.Value]
			if fixture == nil {
				t.Errorf("unexpected transaction info request for %q", body.Value)
				fixture = []byte("{}")
			}
			_, _ = response.Write(fixture)
		default:
			t.Errorf("unexpected TronGrid request %s %s", request.Method, request.URL.String())
			http.NotFound(response, request)
		}
	}))
}

func recordedTransferRequest() analysis.AddressTransferPageRequest {
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	return analysis.AddressTransferPageRequest{
		Address:        recordedTarget,
		Network:        "TRON_MAINNET",
		Asset:          analysis.AssetUSDT,
		WindowStart:    cutoff.Add(-30 * 24 * time.Hour),
		WindowEnd:      cutoff,
		CutoffBlockID:  "0000000003abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TransferLimit:  500,
		TraversalDepth: 2,
	}
}

func newRecordedTronGridProvider(t *testing.T, baseURL string) *analysis.TronGridProvider {
	t.Helper()
	provider, err := analysis.NewTronGridProvider(recordedTronGridConfig(baseURL))
	if err != nil {
		t.Fatalf("create recorded TronGrid provider: %v", err)
	}
	return provider
}

func recordedTronGridConfig(baseURL string) analysis.TronGridConfig {
	return analysis.TronGridConfig{
		APIKey:           "recorded-fixture-key",
		BaseURL:          baseURL,
		HTTPTimeout:      100 * time.Millisecond,
		MaxRetries:       1,
		RetryBaseDelay:   time.Millisecond,
		MaxRetryDelay:    5 * time.Millisecond,
		MaxResponseBytes: 1 << 20,
		PageLimit:        200,
		MaxPages:         25,
	}
}

func assertInterruptionKind(t *testing.T, err error, want analysis.InterruptionKind) {
	t.Helper()
	var interruption *analysis.CollectionInterruption
	if !errors.As(err, &interruption) || interruption.Kind != want {
		t.Fatalf("interruption = %v, want kind %s", err, want)
	}
}

func tronGridFixture(t *testing.T, name string) []byte {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("testdata", "trongrid", name))
	if err != nil {
		t.Fatalf("read TronGrid fixture %s: %v", name, err)
	}
	return fixture
}
