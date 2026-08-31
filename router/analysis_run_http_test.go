package router_test

import (
	"chaintrace/analysis"
	"chaintrace/auth"
	"chaintrace/model"
	"chaintrace/model/store"
	apiRouter "chaintrace/router"
	"chaintrace/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	testTargetAddress = "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"
	testPeerAddress   = "TMwFHYXLJaRUPeW6421aqXL4ZEzPRFGkGT"
	testUSDTContract  = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	testLargeAmount   = "900719925474099312345678"
)

type fixtureChainDataProvider struct {
	cutoff     analysis.BlockCutoff
	collection analysis.Collection
	captureErr error
	collectErr error
	capture    func(context.Context, string) (analysis.BlockCutoff, error)
	collect    func(context.Context, analysis.CollectionRequest) (analysis.Collection, error)
	fetch      func(context.Context, analysis.AddressTransferPageRequest) (analysis.AddressTransferPage, error)
}

func (p *fixtureChainDataProvider) CaptureConfirmedCutoff(ctx context.Context, network string) (analysis.BlockCutoff, error) {
	if p.capture != nil {
		return p.capture(ctx, network)
	}
	return p.cutoff, p.captureErr
}

func (p *fixtureChainDataProvider) Collect(ctx context.Context, request analysis.CollectionRequest) (analysis.Collection, error) {
	if p.collect != nil {
		return p.collect(ctx, request)
	}
	return p.collection, p.collectErr
}

func (p *fixtureChainDataProvider) FetchAddressTransfers(ctx context.Context, request analysis.AddressTransferPageRequest) (analysis.AddressTransferPage, error) {
	if p.fetch != nil {
		return p.fetch(ctx, request)
	}
	if request.Address != testTargetAddress || request.Cursor != "" {
		return analysis.AddressTransferPage{}, nil
	}
	collection, err := p.Collect(ctx, analysis.CollectionRequest{
		TargetAddress:  request.Address,
		Network:        request.Network,
		Asset:          request.Asset,
		WindowStart:    request.WindowStart,
		WindowEnd:      request.WindowEnd,
		CutoffBlockID:  request.CutoffBlockID,
		TransferLimit:  request.TransferLimit,
		TraversalDepth: request.TraversalDepth,
	})
	if err != nil {
		return analysis.AddressTransferPage{}, err
	}
	return analysis.AddressTransferPage{Transactions: collection.Transactions}, nil
}

type fixtureRiskEvaluator struct {
	result analysis.Evaluation
	err    error
}

func (e fixtureRiskEvaluator) Evaluate(context.Context, analysis.EvaluationInput) (analysis.Evaluation, error) {
	return e.result, e.err
}

func TestOwnerCanCompleteOneTransferAnalysisAndReloadCurrentResult(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	score := 0
	provider := &fixtureChainDataProvider{
		cutoff: analysis.BlockCutoff{BlockID: "0000000000012345", Timestamp: cutoff},
		collection: analysis.Collection{
			StopReason:   analysis.StopReasonSourceExhausted,
			Partial:      false,
			Confidence:   100,
			ReachedDepth: 1,
			Transactions: []analysis.BlockchainTransaction{{
				Network:        string(store.NetworkTRONMainnet),
				Hash:           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				BlockID:        "0000000000012345",
				BlockTimestamp: cutoff.Add(-time.Hour),
				Successful:     true,
				Confirmed:      true,
				Transfers: []analysis.TRC20Transfer{{
					EventIdentity:      "log:7",
					ContractAddress:    testUSDTContract,
					Asset:              analysis.AssetUSDT,
					FromAddress:        testTargetAddress,
					ToAddress:          testPeerAddress,
					AmountSmallestUnit: testLargeAmount,
					Decimals:           6,
					Timestamp:          cutoff.Add(-time.Hour),
				}},
			}},
		},
	}
	evaluator := fixtureRiskEvaluator{result: analysis.Evaluation{
		Score:           &score,
		Level:           "low",
		Reasons:         []string{},
		NodeAssessments: []analysis.NodeAssessment{},
		Source:          "rules-v1",
	}}
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: provider, Evaluator: evaluator})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if started.Code != http.StatusAccepted {
		t.Fatalf("start status = %d, want %d; body: %s", started.Code, http.StatusAccepted, started.Body.String())
	}
	var accepted struct {
		ID              string `json:"id"`
		InvestigationID string `json:"investigationId"`
		Status          string `json:"status"`
		TransferLimit   int    `json:"transferLimit"`
		TraversalDepth  int    `json:"traversalDepth"`
	}
	if err := json.Unmarshal(started.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode accepted run: %v", err)
	}
	if accepted.ID == "" || accepted.InvestigationID != investigationID || accepted.Status != "queued" || accepted.TransferLimit != 500 || accepted.TraversalDepth != 2 {
		t.Fatalf("accepted run = %#v", accepted)
	}

	completed := pollAnalysisRun(t, test, token, investigationID, accepted.ID)
	if completed.Status != "completed" || completed.ResultID == nil || *completed.ResultID == "" {
		t.Fatalf("completed run = %#v", completed)
	}
	if completed.Phase != "completed" || completed.CollectedTransfers != 1 || completed.ErrorCode != nil {
		t.Errorf("completed progress = %#v", completed)
	}

	current := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/current-result", nil, token)
	if current.Code != http.StatusOK {
		t.Fatalf("current-result status = %d, want %d; body: %s", current.Code, http.StatusOK, current.Body.String())
	}
	assertOneTransferCurrentResult(t, current.Body.Bytes(), *completed.ResultID, cutoff)
	assertOneTransferSQLRows(t, investigationID, *completed.ResultID)

	detail := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID, nil, token)
	assertCompletedInvestigationSummary(t, detail.Code, detail.Body.Bytes(), *completed.ResultID)
	lockedPatch := test.request(http.MethodPatch, "/api/v1/investigations/"+investigationID, map[string]string{
		"address": "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj",
	}, token)
	if lockedPatch.Code != http.StatusConflict || !strings.Contains(lockedPatch.Body.String(), `"code":"immutable_investigation_target"`) {
		t.Errorf("full-result target patch = %d %s", lockedPatch.Code, lockedPatch.Body.String())
	}

	// The run registry and injected provider are gone; the completed result is still SQL-backed.
	test.engine = gin.New()
	apiRouter.ApiRouter(test.engine)
	reloaded := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/current-result", nil, token)
	if reloaded.Code != http.StatusOK {
		t.Fatalf("reloaded current-result status = %d, want %d; body: %s", reloaded.Code, http.StatusOK, reloaded.Body.String())
	}
	assertOneTransferCurrentResult(t, reloaded.Body.Bytes(), *completed.ResultID, cutoff)
}

func TestAnalysisRunAcceptsMaximumScope(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	requests := make(chan analysis.CollectionRequest, 1)
	provider := successfulProvider(cutoff)
	provider.collect = func(_ context.Context, request analysis.CollectionRequest) (analysis.Collection, error) {
		requests <- request
		return oneTransferCollection(cutoff), nil
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: provider, Evaluator: successfulEvaluator()})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", map[string]int{
		"transferLimit":  5000,
		"traversalDepth": 4,
	}, token)
	if started.Code != http.StatusAccepted {
		t.Fatalf("maximum-scope start status = %d; body: %s", started.Code, started.Body.String())
	}
	var accepted struct {
		TransferLimit  int `json:"transferLimit"`
		TraversalDepth int `json:"traversalDepth"`
	}
	if err := json.Unmarshal(started.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode maximum accepted scope: %v", err)
	}
	if accepted.TransferLimit != 5000 || accepted.TraversalDepth != 4 {
		t.Errorf("accepted scope = %#v", accepted)
	}

	request := <-requests
	if request.TransferLimit != 5000 || request.TraversalDepth != 4 {
		t.Errorf("collection scope = %#v", request)
	}
	if !request.WindowEnd.Equal(cutoff) || !request.WindowStart.Equal(cutoff.Add(-30*24*time.Hour)) {
		t.Errorf("collection window = %s through %s", request.WindowStart, request.WindowEnd)
	}
	completed := pollAnalysisRun(t, test, token, investigationID, decodeRunID(t, started.Body.Bytes()))
	if completed.Status != "completed" || completed.ResultID == nil || completed.TransferLimit != 5000 || completed.TraversalDepth != 4 {
		t.Fatalf("maximum-scope run = %#v", completed)
	}
	var dataset store.AnalysisDataset
	if err := model.DB.First(&dataset, "id = ?", *completed.ResultID).Error; err != nil {
		t.Fatalf("reload maximum-scope dataset: %v", err)
	}
	if dataset.TransferLimit != 5000 || dataset.TraversalDepth != 4 {
		t.Errorf("persisted maximum scope = %#v", dataset)
	}
}

func TestAnalysisRunRejectsInvalidScopeWithoutStartingRun(t *testing.T) {
	invalidScopes := []map[string]int{
		{"transferLimit": 0},
		{"transferLimit": 5001},
		{"traversalDepth": 0},
		{"traversalDepth": 5},
	}
	for _, scope := range invalidScopes {
		t.Run(fmt.Sprint(scope), func(t *testing.T) {
			test := newAuthHTTPTest(t)
			migrateAnalysisTestTables(t)
			provider := successfulProvider(time.Now().UTC())
			var captureCalls atomic.Int32
			provider.capture = func(context.Context, string) (analysis.BlockCutoff, error) {
				captureCalls.Add(1)
				return provider.cutoff, nil
			}
			test.engine = gin.New()
			apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: provider, Evaluator: successfulEvaluator()})
			token := ownerToken(t, test.owner.ID)
			investigationID := createTargetedInvestigation(t, test, token)

			response := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", scope, token)

			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_analysis_scope"`) {
				t.Fatalf("invalid-scope response = %d %s", response.Code, response.Body.String())
			}
			time.Sleep(10 * time.Millisecond)
			if captureCalls.Load() != 0 {
				t.Errorf("invalid scope started provider %d times", captureCalls.Load())
			}
			var investigation store.Investigation
			if err := model.DB.First(&investigation, "id = ?", investigationID).Error; err != nil {
				t.Fatalf("reload invalid-scope investigation: %v", err)
			}
			if investigation.Status != store.InvestigationPending || investigation.CurrentResultID != nil {
				t.Errorf("invalid scope changed investigation = %#v", investigation)
			}
		})
	}
}

func TestAnalysisRunRequiresAnInvestigationTarget(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: successfulProvider(time.Now().UTC()), Evaluator: successfulEvaluator()})
	token := ownerToken(t, test.owner.ID)
	created := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": "No target"}, token)
	if created.Code != http.StatusCreated {
		t.Fatalf("create no-target investigation status = %d; body: %s", created.Code, created.Body.String())
	}
	var investigation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &investigation); err != nil {
		t.Fatalf("decode no-target investigation: %v", err)
	}

	response := test.request(http.MethodPost, "/api/v1/investigations/"+investigation.ID+"/analysis-runs", nil, token)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"investigation_target_required"`) {
		t.Fatalf("no-target start status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	assertNoPublishedAnalysis(t)
	var saved store.Investigation
	if err := model.DB.First(&saved, "id = ?", investigation.ID).Error; err != nil {
		t.Fatalf("reload no-target investigation: %v", err)
	}
	if saved.Status != store.InvestigationPending || saved.CurrentResultID != nil {
		t.Errorf("no-target investigation changed = %#v", saved)
	}
}

func TestOnlyOneAnalysisRunCanBeActivePerInvestigation(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	provider := successfulProvider(cutoff)
	provider.collect = func(ctx context.Context, _ analysis.CollectionRequest) (analysis.Collection, error) {
		close(entered)
		select {
		case <-release:
			return oneTransferCollection(cutoff), nil
		case <-ctx.Done():
			return analysis.Collection{}, ctx.Err()
		}
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: provider, Evaluator: successfulEvaluator()})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	first := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first start status = %d; body: %s", first.Code, first.Body.String())
	}
	var accepted struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode first run: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("blocking provider was not entered")
	}
	running := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+accepted.ID, nil, token)
	if running.Code != http.StatusOK || !strings.Contains(running.Body.String(), `"status":"running"`) || !strings.Contains(running.Body.String(), `"phase":"collecting"`) {
		t.Errorf("blocked run status = %d; body: %s", running.Code, running.Body.String())
	}

	second := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), `"code":"analysis_run_active"`) {
		t.Errorf("second start status = %d, want %d; body: %s", second.Code, http.StatusConflict, second.Body.String())
	}
	// A client that did not start this run still has to be able to watch it:
	// the Agent can start one on the Owner's behalf, and the Investigation is
	// the only place that discloses its id.
	if got := investigationActiveRun(t, test, token, investigationID); got == nil || *got != accepted.ID {
		t.Errorf("activeRun while collecting = %v, want %q", got, accepted.ID)
	}

	releaseOnce.Do(func() { close(release) })
	completed := pollAnalysisRun(t, test, token, investigationID, accepted.ID)
	if completed.Status != "completed" {
		t.Fatalf("first run after release = %#v", completed)
	}
	if got := investigationActiveRun(t, test, token, investigationID); got != nil {
		t.Errorf("activeRun after publication = %q, want null", *got)
	}
}

func investigationActiveRun(t *testing.T, test *authHTTPTest, token, investigationID string) *string {
	t.Helper()
	response := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID, nil, token)
	if response.Code != http.StatusOK {
		t.Fatalf("investigation detail status = %d; body: %s", response.Code, response.Body.String())
	}
	var got struct {
		ActiveRun *string `json:"activeRun"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode investigation detail: %v", err)
	}
	return got.ActiveRun
}

func TestAnalysisRunAndCurrentResultAreOwnerScoped(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: successfulProvider(cutoff), Evaluator: successfulEvaluator()})
	primaryToken := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, primaryToken)
	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, primaryToken)
	if started.Code != http.StatusAccepted {
		t.Fatalf("owner start status = %d; body: %s", started.Code, started.Body.String())
	}
	var accepted struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(started.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode owner run: %v", err)
	}
	if completed := pollAnalysisRun(t, test, primaryToken, investigationID, accepted.ID); completed.Status != "completed" {
		t.Fatalf("owner run = %#v", completed)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	other := store.User{
		Username: "analysis-other-" + suffix,
		Email:    "analysis-other-" + suffix + "@auth-test.invalid",
		Password: "test-only",
		Salt:     "test-only",
	}
	if err := model.DB.Create(&other).Error; err != nil {
		t.Fatalf("create other analysis owner: %v", err)
	}
	otherToken := ownerToken(t, other.ID)

	crossStart := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, otherToken)
	missingStart := test.request(http.MethodPost, "/api/v1/investigations/missingInvestigation00000/analysis-runs", nil, otherToken)
	if crossStart.Code != http.StatusNotFound || missingStart.Code != http.StatusNotFound || crossStart.Body.String() != missingStart.Body.String() {
		t.Errorf("cross/missing starts = (%d %s) / (%d %s)", crossStart.Code, crossStart.Body.String(), missingStart.Code, missingStart.Body.String())
	}
	crossPoll := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+accepted.ID, nil, otherToken)
	missingPoll := test.request(http.MethodGet, "/api/v1/investigations/missingInvestigation00000/analysis-runs/missingRun00000000000000", nil, otherToken)
	if crossPoll.Code != http.StatusNotFound || missingPoll.Code != http.StatusNotFound || crossPoll.Body.String() != missingPoll.Body.String() {
		t.Errorf("cross/missing polls = (%d %s) / (%d %s)", crossPoll.Code, crossPoll.Body.String(), missingPoll.Code, missingPoll.Body.String())
	}
	crossResult := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/current-result", nil, otherToken)
	missingResult := test.request(http.MethodGet, "/api/v1/investigations/missingInvestigation00000/current-result", nil, otherToken)
	if crossResult.Code != http.StatusNotFound || missingResult.Code != http.StatusNotFound || crossResult.Body.String() != missingResult.Body.String() {
		t.Errorf("cross/missing current results = (%d %s) / (%d %s)", crossResult.Code, crossResult.Body.String(), missingResult.Code, missingResult.Body.String())
	}
}

func TestProductionRouterDoesNotUseAFixtureProvider(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	test.engine = gin.New()
	apiRouter.ApiRouter(test.engine)
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if started.Code != http.StatusAccepted {
		t.Fatalf("unconfigured start status = %d; body: %s", started.Code, started.Body.String())
	}
	runID := decodeRunID(t, started.Body.Bytes())
	failed := pollAnalysisRun(t, test, token, investigationID, runID)
	if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != "provider_unavailable" {
		t.Fatalf("unconfigured provider run = %#v", failed)
	}
	assertFailedFirstRunLeftNoPublication(t, investigationID)
}

func TestProductionTronGridRunPublishesRecordedAdapterOutput(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	fixtures := map[string][]byte{
		"cutoff":                tronGridRouterFixture(t, "cutoff.json"),
		"page":                  tronGridRouterFixture(t, "trc20-page-2.json"),
		strings.Repeat("a", 64): tronGridRouterFixture(t, "transaction-info-multi.json"),
		strings.Repeat("9", 64): tronGridRouterFixture(t, "transaction-info-new.json"),
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("TRON-PRO-API-KEY") != "production-recorded-key" {
			t.Errorf("production API key header = %q", request.Header.Get("TRON-PRO-API-KEY"))
		}
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/walletsolidity/getnowblock":
			_, _ = response.Write(fixtures["cutoff"])
		case request.Method == http.MethodGet && request.URL.Path == "/v1/accounts/"+testTargetAddress+"/transactions/trc20":
			_, _ = response.Write(fixtures["page"])
		case request.Method == http.MethodGet && request.URL.Path == "/v1/accounts/"+analysis.TRONMainnetUSDTContract+"/transactions/trc20":
			_, _ = response.Write([]byte(`{"data":[],"success":true,"meta":{"page_size":0}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/walletsolidity/gettransactioninfobyid":
			var body struct {
				Value string `json:"value"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode production transaction info request: %v", err)
			}
			fixture := fixtures[body.Value]
			if fixture == nil {
				t.Errorf("unexpected production transaction %q", body.Value)
				fixture = []byte("{}")
			}
			_, _ = response.Write(fixture)
		default:
			t.Errorf("unexpected production TronGrid request %s %s", request.Method, request.URL.String())
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	restoreTronGridConfig := setRecordedProductionTronGridConfig(server.URL)
	defer restoreTronGridConfig()
	test.engine = gin.New()
	apiRouter.ApiRouter(test.engine)
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if started.Code != http.StatusAccepted {
		t.Fatalf("production TronGrid start = %d %s", started.Code, started.Body.String())
	}
	completed := pollAnalysisRun(t, test, token, investigationID, decodeRunID(t, started.Body.Bytes()))
	if completed.Status != "completed" || completed.ResultID == nil || completed.CollectedTransfers != 2 {
		t.Fatalf("production TronGrid run = %#v", completed)
	}
	var transfers []store.TRC20Transfer
	if err := model.DB.Order("transaction_hash, event_identity").Find(&transfers, "dataset_id = ?", *completed.ResultID).Error; err != nil {
		t.Fatalf("load production TronGrid transfers: %v", err)
	}
	if len(transfers) != 2 || transfers[0].EventIdentity != "log:0" || transfers[0].AmountSmallestUnit != "42" ||
		transfers[1].EventIdentity != "log:1" || transfers[1].AmountSmallestUnit != testLargeAmount {
		t.Errorf("published production TronGrid transfers = %#v", transfers)
	}
	var metrics store.InvestigationMetrics
	if err := model.DB.First(&metrics, "dataset_id = ?", *completed.ResultID).Error; err != nil {
		t.Fatalf("load production TronGrid metrics: %v", err)
	}
	if metrics.TransferCount != 2 || metrics.TotalFlowSmallestUnit != "900719925474099312345720" || metrics.TotalFlowDecimals != 6 || metrics.Asset != analysis.AssetUSDT {
		t.Errorf("published production TronGrid metrics = %#v", metrics)
	}
}

func TestProviderFailurePublishesNothing(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	provider := successfulProvider(cutoff)
	provider.collectErr = errors.New("fixture provider failed")
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: provider, Evaluator: successfulEvaluator()})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	failed := pollAnalysisRun(t, test, token, investigationID, decodeRunID(t, started.Body.Bytes()))
	if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != "provider_failure" {
		t.Fatalf("provider failure run = %#v", failed)
	}
	assertFailedFirstRunLeftNoPublication(t, investigationID)
}

func TestTypedProviderInterruptionPublishesUsableEvidenceAsPartial(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	provider := successfulProvider(cutoff)
	provider.fetch = func(_ context.Context, request analysis.AddressTransferPageRequest) (analysis.AddressTransferPage, error) {
		if request.Address != testTargetAddress {
			return analysis.AddressTransferPage{}, nil
		}
		if request.Cursor == "next" {
			return analysis.AddressTransferPage{}, &analysis.CollectionInterruption{
				Kind: analysis.InterruptionRateLimited,
				Err:  errors.New("fixture rate limit"),
			}
		}
		return analysis.AddressTransferPage{
			Transactions: oneTransferCollection(cutoff).Transactions,
			NextCursor:   "next",
		}, nil
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: provider, Evaluator: successfulEvaluator()})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", map[string]int{
		"transferLimit": 10,
	}, token)
	completed := pollAnalysisRun(t, test, token, investigationID, decodeRunID(t, started.Body.Bytes()))
	if completed.Status != "completed" || completed.ResultID == nil {
		t.Fatalf("partial run = %#v", completed)
	}

	current := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/current-result", nil, token)
	if current.Code != http.StatusOK {
		t.Fatalf("partial current result = %d %s", current.Code, current.Body.String())
	}
	var result struct {
		Dataset struct {
			TransferLimit      int    `json:"transferLimit"`
			TraversalDepth     int    `json:"traversalDepth"`
			CollectedTransfers int    `json:"collectedTransfers"`
			ReachedDepth       int    `json:"reachedDepth"`
			Partial            bool   `json:"partial"`
			Confidence         int    `json:"confidence"`
			StopReason         string `json:"stopReason"`
		} `json:"dataset"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode partial result: %v", err)
	}
	if result.Dataset.TransferLimit != 10 || result.Dataset.TraversalDepth != 2 || result.Dataset.CollectedTransfers != 1 || result.Dataset.ReachedDepth != 1 ||
		!result.Dataset.Partial || result.Dataset.Confidence != 10 || result.Dataset.StopReason != analysis.StopReasonProviderRateLimited {
		t.Errorf("partial coverage = %#v", result.Dataset)
	}
	var dataset store.AnalysisDataset
	if err := model.DB.First(&dataset, "id = ?", *completed.ResultID).Error; err != nil {
		t.Fatalf("reload partial dataset: %v", err)
	}
	if dataset.CollectedTransfers != 1 || dataset.ReachedDepth != 1 || !dataset.Partial || dataset.Confidence != 10 || dataset.StopReason != analysis.StopReasonProviderRateLimited {
		t.Errorf("persisted partial coverage = %#v", dataset)
	}
	var investigation store.Investigation
	if err := model.DB.First(&investigation, "id = ?", investigationID).Error; err != nil {
		t.Fatalf("reload partial investigation: %v", err)
	}
	if investigation.Status != store.InvestigationCompleted || !investigation.TargetLocked || investigation.CurrentResultID == nil || *investigation.CurrentResultID != *completed.ResultID {
		t.Errorf("partial publication lifecycle = %#v", investigation)
	}
	patch := test.request(http.MethodPatch, "/api/v1/investigations/"+investigationID, map[string]string{
		"address": "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj",
	}, token)
	if patch.Code != http.StatusConflict || !strings.Contains(patch.Body.String(), `"code":"immutable_investigation_target"`) {
		t.Errorf("partial target patch = %d %s", patch.Code, patch.Body.String())
	}
}

func TestTypedProviderInterruptionWithoutEvidenceFailsRun(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	provider := successfulProvider(time.Now().UTC())
	provider.fetch = func(context.Context, analysis.AddressTransferPageRequest) (analysis.AddressTransferPage, error) {
		return analysis.AddressTransferPage{}, &analysis.CollectionInterruption{Kind: analysis.InterruptionUnavailable, Err: errors.New("fixture unavailable")}
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: provider})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	failed := pollAnalysisRun(t, test, token, investigationID, decodeRunID(t, started.Body.Bytes()))
	if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != analysis.StopReasonProviderUnavailable {
		t.Fatalf("empty interrupted run = %#v", failed)
	}
	assertFailedFirstRunLeftNoPublication(t, investigationID)
}

func TestZeroEligibleTransfersPublishesInsufficientEvidence(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	provider := successfulProvider(cutoff)
	provider.fetch = func(context.Context, analysis.AddressTransferPageRequest) (analysis.AddressTransferPage, error) {
		return analysis.AddressTransferPage{}, nil
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: provider})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	completed := pollAnalysisRun(t, test, token, investigationID, decodeRunID(t, started.Body.Bytes()))
	if completed.Status != "completed" || completed.ResultID == nil || completed.CollectedTransfers != 0 {
		t.Fatalf("zero-evidence run = %#v", completed)
	}

	current := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/current-result", nil, token)
	if current.Code != http.StatusOK {
		t.Fatalf("zero-evidence current result = %d %s", current.Code, current.Body.String())
	}
	var result struct {
		Dataset struct {
			CollectedTransfers int    `json:"collectedTransfers"`
			ReachedDepth       int    `json:"reachedDepth"`
			Partial            bool   `json:"partial"`
			Confidence         int    `json:"confidence"`
			StopReason         string `json:"stopReason"`
		} `json:"dataset"`
		Metrics struct {
			TransferCount int                  `json:"transferCount"`
			TotalFlow     analysis.ExactAmount `json:"totalFlow"`
		} `json:"metrics"`
		Assessment struct {
			Score   *int     `json:"score"`
			Level   string   `json:"level"`
			Source  string   `json:"source"`
			Reasons []string `json:"reasons"`
		} `json:"assessment"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode zero-evidence result: %v", err)
	}
	if result.Dataset.CollectedTransfers != 0 || result.Dataset.ReachedDepth != 0 || result.Dataset.Partial || result.Dataset.Confidence != 0 || result.Dataset.StopReason != analysis.StopReasonNoEligibleTransfers {
		t.Errorf("zero-evidence coverage = %#v", result.Dataset)
	}
	if result.Metrics.TransferCount != 0 || result.Metrics.TotalFlow.SmallestUnit != "0" || result.Metrics.TotalFlow.Decimals != 6 || result.Metrics.TotalFlow.Asset != analysis.AssetUSDT {
		t.Errorf("zero-evidence metrics = %#v", result.Metrics)
	}
	if result.Assessment.Score != nil || result.Assessment.Level != "" || result.Assessment.Source != "rules-v1" || len(result.Assessment.Reasons) != 0 {
		t.Errorf("zero-evidence assessment = %#v", result.Assessment)
	}

	var assessment store.Assessment
	if err := model.DB.First(&assessment, "dataset_id = ?", *completed.ResultID).Error; err != nil {
		t.Fatalf("reload zero-evidence assessment: %v", err)
	}
	if assessment.Score != nil || assessment.Level != "" {
		t.Errorf("persisted zero-evidence assessment = %#v", assessment)
	}
	var investigation store.Investigation
	if err := model.DB.First(&investigation, "id = ?", investigationID).Error; err != nil {
		t.Fatalf("reload zero-evidence investigation: %v", err)
	}
	if investigation.Status != store.InvestigationCompleted || !investigation.TargetLocked || investigation.CurrentResultID == nil || investigation.RiskScore != nil {
		t.Errorf("zero-evidence investigation = %#v", investigation)
	}
	detail := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID, nil, token)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"risk":null`) || !strings.Contains(detail.Body.String(), `"currentResult":"`+*completed.ResultID+`"`) {
		t.Errorf("zero-evidence investigation summary = %d %s", detail.Code, detail.Body.String())
	}
}

func TestEvaluatorFailurePublishesNothing(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  successfulProvider(cutoff),
		Evaluator: fixtureRiskEvaluator{err: errors.New("fixture evaluator failed")},
	})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	failed := pollAnalysisRun(t, test, token, investigationID, decodeRunID(t, started.Body.Bytes()))
	if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != "evaluation_failed" {
		t.Fatalf("evaluator failure run = %#v", failed)
	}
	assertFailedFirstRunLeftNoPublication(t, investigationID)
}

func TestPublicationRollbackPreservesPreviousStableResult(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: successfulProvider(cutoff), Evaluator: successfulEvaluator()})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)
	firstStart := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	first := pollAnalysisRun(t, test, token, investigationID, decodeRunID(t, firstStart.Body.Bytes()))
	if first.Status != "completed" || first.ResultID == nil {
		t.Fatalf("stable first run = %#v", first)
	}
	stableResultID := *first.ResultID

	badScore := 101
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider: successfulProvider(cutoff.Add(time.Hour)),
		Evaluator: fixtureRiskEvaluator{result: analysis.Evaluation{
			Score:   &badScore,
			Level:   "critical",
			Source:  "rules-v1",
			Reasons: []string{"invalid-fixture"},
		}},
	})

	secondStart := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	second := pollAnalysisRun(t, test, token, investigationID, decodeRunID(t, secondStart.Body.Bytes()))
	if second.Status != "failed" || second.ErrorCode == nil || *second.ErrorCode != "publication_failed" {
		t.Fatalf("publication failure run = %#v", second)
	}

	current := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/current-result", nil, token)
	if current.Code != http.StatusOK {
		t.Fatalf("stable current result after rollback status = %d; body: %s", current.Code, current.Body.String())
	}
	var result struct {
		Dataset struct {
			ID string `json:"id"`
		} `json:"dataset"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode stable result after rollback: %v", err)
	}
	if result.Dataset.ID != stableResultID {
		t.Errorf("current result after rollback = %q, want %q", result.Dataset.ID, stableResultID)
	}
	assertOneTransferSQLRows(t, investigationID, stableResultID)
	var datasetCount int64
	if err := model.DB.Model(&store.AnalysisDataset{}).Count(&datasetCount).Error; err != nil {
		t.Fatalf("count datasets after rollback: %v", err)
	}
	if datasetCount != 1 {
		t.Errorf("dataset count after rollback = %d, want previous 1", datasetCount)
	}
}

type analysisRunResponse struct {
	ID                 string  `json:"id"`
	InvestigationID    string  `json:"investigationId"`
	Status             string  `json:"status"`
	Phase              string  `json:"phase"`
	TransferLimit      int     `json:"transferLimit"`
	TraversalDepth     int     `json:"traversalDepth"`
	CollectedTransfers int     `json:"collectedTransfers"`
	ResultID           *string `json:"resultId"`
	ErrorCode          *string `json:"errorCode"`
}

func migrateAnalysisTestTables(t *testing.T) {
	t.Helper()
	if err := model.DB.AutoMigrate(
		&store.AnalysisDataset{},
		&store.BlockchainTransaction{},
		&store.TRC20Transfer{},
		&store.InvestigationMetrics{},
		&store.Assessment{},
	); err != nil {
		t.Fatalf("migrate analysis test tables: %v", err)
	}
}

func successfulProvider(cutoff time.Time) *fixtureChainDataProvider {
	return &fixtureChainDataProvider{
		cutoff:     analysis.BlockCutoff{BlockID: "0000000000012345", Timestamp: cutoff},
		collection: oneTransferCollection(cutoff),
	}
}

func oneTransferCollection(cutoff time.Time) analysis.Collection {
	return analysis.Collection{
		StopReason:   analysis.StopReasonSourceExhausted,
		Partial:      false,
		Confidence:   100,
		ReachedDepth: 1,
		Transactions: []analysis.BlockchainTransaction{{
			Network:        string(store.NetworkTRONMainnet),
			Hash:           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			BlockID:        "0000000000012345",
			BlockTimestamp: cutoff.Add(-time.Hour),
			Successful:     true,
			Confirmed:      true,
			Transfers: []analysis.TRC20Transfer{{
				EventIdentity:      "log:7",
				ContractAddress:    testUSDTContract,
				Asset:              analysis.AssetUSDT,
				FromAddress:        testTargetAddress,
				ToAddress:          testPeerAddress,
				AmountSmallestUnit: testLargeAmount,
				Decimals:           6,
				Timestamp:          cutoff.Add(-time.Hour),
			}},
		}},
	}
}

func successfulEvaluator() fixtureRiskEvaluator {
	score := 0
	return fixtureRiskEvaluator{result: analysis.Evaluation{
		Score:           &score,
		Level:           "low",
		Reasons:         []string{},
		NodeAssessments: []analysis.NodeAssessment{},
		Source:          "rules-v1",
	}}
}

func ownerToken(t *testing.T, ownerID uint) string {
	t.Helper()
	token, err := auth.GenJWT(ownerID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	return token
}

func decodeRunID(t *testing.T, body []byte) string {
	t.Helper()
	var run struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &run); err != nil {
		t.Fatalf("decode run ID: %v; body: %s", err, body)
	}
	if run.ID == "" {
		t.Fatalf("run ID is empty; body: %s", body)
	}
	return run.ID
}

func createTargetedInvestigation(t *testing.T, test *authHTTPTest, token string) string {
	t.Helper()
	response := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{
		"title":   "One transfer tracer",
		"address": testTargetAddress,
	}, token)
	if response.Code != http.StatusCreated {
		t.Fatalf("create targeted investigation status = %d; body: %s", response.Code, response.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode targeted investigation: %v", err)
	}
	return created.ID
}

func pollAnalysisRun(t *testing.T, test *authHTTPTest, token, investigationID, runID string) analysisRunResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+runID, nil, token)
		if response.Code != http.StatusOK {
			t.Fatalf("poll status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
		}
		var run analysisRunResponse
		if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
			t.Fatalf("decode run: %v", err)
		}
		if run.Status == "completed" || run.Status == "failed" {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("analysis run %s did not finish", runID)
	return analysisRunResponse{}
}

func assertOneTransferCurrentResult(t *testing.T, body []byte, datasetID string, cutoff time.Time) {
	t.Helper()
	var got struct {
		Dataset struct {
			ID                 string    `json:"id"`
			Network            string    `json:"network"`
			Asset              string    `json:"asset"`
			WindowStart        time.Time `json:"windowStart"`
			WindowEnd          time.Time `json:"windowEnd"`
			CutoffBlockID      string    `json:"cutoffBlockId"`
			TransferLimit      int       `json:"transferLimit"`
			TraversalDepth     int       `json:"traversalDepth"`
			CollectedTransfers int       `json:"collectedTransfers"`
			ReachedDepth       int       `json:"reachedDepth"`
			Partial            bool      `json:"partial"`
			Confidence         int       `json:"confidence"`
			StopReason         string    `json:"stopReason"`
		} `json:"dataset"`
		Metrics struct {
			RelatedNodes  int `json:"relatedNodes"`
			TransferCount int `json:"transferCount"`
			TotalFlow     struct {
				SmallestUnit string `json:"smallestUnit"`
				Decimals     int    `json:"decimals"`
				Asset        string `json:"asset"`
			} `json:"totalFlow"`
		} `json:"metrics"`
		Assessment struct {
			Score           *int                      `json:"score"`
			Level           string                    `json:"level"`
			Reasons         []string                  `json:"reasons"`
			NodeAssessments []analysis.NodeAssessment `json:"nodeAssessments"`
			Source          string                    `json:"source"`
		} `json:"assessment"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode current result: %v; body: %s", err, body)
	}
	if got.Dataset.ID != datasetID || got.Dataset.Network != "TRON_MAINNET" || got.Dataset.Asset != analysis.AssetUSDT {
		t.Errorf("current dataset identity = %#v", got.Dataset)
	}
	if !got.Dataset.WindowEnd.Equal(cutoff) || !got.Dataset.WindowStart.Equal(cutoff.Add(-30*24*time.Hour)) {
		t.Errorf("analysis window = %s through %s, want 30 days ending %s", got.Dataset.WindowStart, got.Dataset.WindowEnd, cutoff)
	}
	if got.Dataset.CutoffBlockID != "0000000000012345" {
		t.Errorf("analysis cutoff = %q", got.Dataset.CutoffBlockID)
	}
	if got.Dataset.TransferLimit != 500 || got.Dataset.TraversalDepth != 2 || got.Dataset.CollectedTransfers != 1 || got.Dataset.ReachedDepth != 1 || got.Dataset.Partial || got.Dataset.Confidence != 100 || got.Dataset.StopReason != "source_exhausted" {
		t.Errorf("dataset scope/coverage = %#v", got.Dataset)
	}
	if got.Metrics.RelatedNodes != 1 || got.Metrics.TransferCount != 1 {
		t.Errorf("metrics counts = %#v", got.Metrics)
	}
	if got.Metrics.TotalFlow.SmallestUnit != testLargeAmount || got.Metrics.TotalFlow.Decimals != 6 || got.Metrics.TotalFlow.Asset != analysis.AssetUSDT {
		t.Errorf("exact total flow = %#v", got.Metrics.TotalFlow)
	}
	if got.Assessment.Score == nil || *got.Assessment.Score != 0 || got.Assessment.Level != "low" || got.Assessment.Source != "rules-v1" {
		t.Errorf("assessment = %#v", got.Assessment)
	}
	if len(got.Assessment.Reasons) != 0 || len(got.Assessment.NodeAssessments) != 0 {
		t.Errorf("single-transfer reasons/nodes = %#v / %#v", got.Assessment.Reasons, got.Assessment.NodeAssessments)
	}
}

func assertOneTransferSQLRows(t *testing.T, investigationID, datasetID string) {
	t.Helper()
	var dataset store.AnalysisDataset
	if err := model.DB.First(&dataset, "id = ?", datasetID).Error; err != nil {
		t.Fatalf("reload dataset SQL row: %v", err)
	}
	if dataset.InvestigationID != investigationID || dataset.CutoffBlockID != "0000000000012345" || dataset.TransferLimit != 500 || dataset.TraversalDepth != 2 {
		t.Errorf("persisted dataset scope = %#v", dataset)
	}
	if dataset.CollectedTransfers != 1 || dataset.ReachedDepth != 1 || dataset.Partial || dataset.Confidence != 100 || dataset.StopReason != analysis.StopReasonSourceExhausted {
		t.Errorf("persisted dataset coverage = %#v", dataset)
	}
	for name, value := range map[string]any{
		"transaction": &store.BlockchainTransaction{},
		"transfer":    &store.TRC20Transfer{},
		"metrics":     &store.InvestigationMetrics{},
		"assessment":  &store.Assessment{},
	} {
		var count int64
		if err := model.DB.Model(value).Where("dataset_id = ?", datasetID).Count(&count).Error; err != nil {
			t.Fatalf("count %s SQL rows: %v", name, err)
		}
		if count != 1 {
			t.Errorf("%s SQL row count = %d, want 1", name, count)
		}
	}
	var investigation store.Investigation
	if err := model.DB.First(&investigation, "id = ?", investigationID).Error; err != nil {
		t.Fatalf("reload completed investigation: %v", err)
	}
	if investigation.CurrentResultID == nil || *investigation.CurrentResultID != datasetID || investigation.Status != store.InvestigationCompleted || !investigation.TargetLocked {
		t.Errorf("completed SQL investigation = %#v", investigation)
	}
	var transaction store.BlockchainTransaction
	if err := model.DB.First(&transaction, "dataset_id = ?", datasetID).Error; err != nil {
		t.Fatalf("reload blockchain transaction: %v", err)
	}
	if transaction.Network != store.NetworkTRONMainnet || transaction.TransactionHash == "" || transaction.BlockID != dataset.CutoffBlockID || !transaction.Successful || !transaction.Confirmed {
		t.Errorf("persisted blockchain transaction = %#v", transaction)
	}
	var transfer store.TRC20Transfer
	if err := model.DB.First(&transfer, "dataset_id = ?", datasetID).Error; err != nil {
		t.Fatalf("reload exact transfer: %v", err)
	}
	if transfer.TransactionHash == "" || transfer.EventIdentity != "log:7" || transfer.AmountSmallestUnit != testLargeAmount || transfer.Decimals != 6 {
		t.Errorf("persisted transfer = %#v", transfer)
	}
}

func assertNoPublishedAnalysis(t *testing.T) {
	t.Helper()
	for name, value := range map[string]any{
		"dataset":     &store.AnalysisDataset{},
		"transaction": &store.BlockchainTransaction{},
		"transfer":    &store.TRC20Transfer{},
		"metrics":     &store.InvestigationMetrics{},
		"assessment":  &store.Assessment{},
	} {
		var count int64
		if err := model.DB.Model(value).Count(&count).Error; err != nil {
			t.Fatalf("count %s rows after failed analysis: %v", name, err)
		}
		if count != 0 {
			t.Errorf("%s rows after failed analysis = %d, want 0", name, count)
		}
	}
}

func assertFailedFirstRunLeftNoPublication(t *testing.T, investigationID string) {
	t.Helper()
	assertNoPublishedAnalysis(t)
	var investigation store.Investigation
	if err := model.DB.First(&investigation, "id = ?", investigationID).Error; err != nil {
		t.Fatalf("reload investigation after failed run: %v", err)
	}
	if investigation.Status != store.InvestigationPending || investigation.CurrentResultID != nil || investigation.TargetLocked {
		t.Errorf("investigation after failed first run = %#v", investigation)
	}
	if investigation.RiskScore != nil || investigation.TotalFlowSmallestUnit != nil || investigation.FlowAsset != nil || investigation.RelatedNodes != 0 || investigation.TransactionCount != 0 {
		t.Errorf("investigation summary after failed first run = %#v", investigation)
	}
}

func assertCompletedInvestigationSummary(t *testing.T, status int, body []byte, datasetID string) {
	t.Helper()
	if status != http.StatusOK {
		t.Fatalf("investigation detail status = %d; body: %s", status, body)
	}
	var got struct {
		Status           string  `json:"status"`
		Risk             *int    `json:"risk"`
		RelatedNodes     int     `json:"relatedNodes"`
		TransactionCount int     `json:"transactionCount"`
		FlowAsset        *string `json:"flowAsset"`
		CurrentResult    *string `json:"currentResult"`
		TotalFlow        *struct {
			SmallestUnit string `json:"smallestUnit"`
			Decimals     int    `json:"decimals"`
			Asset        string `json:"asset"`
		} `json:"totalFlow"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode investigation summary: %v", err)
	}
	if got.Status != "已完成" || got.Risk == nil || *got.Risk != 0 || got.RelatedNodes != 1 || got.TransactionCount != 1 {
		t.Errorf("investigation summary = %#v", got)
	}
	if got.CurrentResult == nil || *got.CurrentResult != datasetID || got.FlowAsset == nil || *got.FlowAsset != analysis.AssetUSDT {
		t.Errorf("investigation result/asset summary = %#v", got)
	}
	if got.TotalFlow == nil || got.TotalFlow.SmallestUnit != testLargeAmount || got.TotalFlow.Decimals != 6 || got.TotalFlow.Asset != analysis.AssetUSDT {
		t.Errorf("investigation exact total summary = %#v", got.TotalFlow)
	}
}

func tronGridRouterFixture(t *testing.T, name string) []byte {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "analysis", "testdata", "trongrid", name))
	if err != nil {
		t.Fatalf("read router TronGrid fixture %s: %v", name, err)
	}
	return fixture
}

func setRecordedProductionTronGridConfig(baseURL string) func() {
	previousAPIKey := utils.TronGridAPIKey
	previousBaseURL := utils.TronGridBaseURL
	previousTimeout := utils.TronGridHTTPTimeout
	previousRetries := utils.TronGridMaxRetries
	previousBaseDelay := utils.TronGridRetryBaseDelay
	previousMaxDelay := utils.TronGridMaxRetryDelay
	previousResponseBytes := utils.TronGridMaxResponseBytes
	previousPageLimit := utils.TronGridPageLimit
	previousMaxPages := utils.TronGridMaxPages
	utils.TronGridAPIKey = "production-recorded-key"
	utils.TronGridBaseURL = baseURL
	utils.TronGridHTTPTimeout = 100 * time.Millisecond
	utils.TronGridMaxRetries = 1
	utils.TronGridRetryBaseDelay = time.Millisecond
	utils.TronGridMaxRetryDelay = 5 * time.Millisecond
	utils.TronGridMaxResponseBytes = 1 << 20
	utils.TronGridPageLimit = 200
	utils.TronGridMaxPages = 25
	return func() {
		utils.TronGridAPIKey = previousAPIKey
		utils.TronGridBaseURL = previousBaseURL
		utils.TronGridHTTPTimeout = previousTimeout
		utils.TronGridMaxRetries = previousRetries
		utils.TronGridRetryBaseDelay = previousBaseDelay
		utils.TronGridMaxRetryDelay = previousMaxDelay
		utils.TronGridMaxResponseBytes = previousResponseBytes
		utils.TronGridPageLimit = previousPageLimit
		utils.TronGridMaxPages = previousMaxPages
	}
}
