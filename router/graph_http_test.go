package router_test

import (
	"chaintrace/analysis"
	"chaintrace/model"
	"chaintrace/model/store"
	apiRouter "chaintrace/router"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type graphFixtureProvider struct {
	cutoff       time.Time
	transactions []analysis.BlockchainTransaction
	calls        atomic.Int32
}

func (p *graphFixtureProvider) CaptureConfirmedCutoff(context.Context, string) (analysis.BlockCutoff, error) {
	p.calls.Add(1)
	return analysis.BlockCutoff{BlockID: "graph-cutoff", Timestamp: p.cutoff}, nil
}

func (p *graphFixtureProvider) FetchAddressTransfers(_ context.Context, request analysis.AddressTransferPageRequest) (analysis.AddressTransferPage, error) {
	p.calls.Add(1)
	if request.Address != testTargetAddress || request.Cursor != "" {
		return analysis.AddressTransferPage{}, nil
	}
	return analysis.AddressTransferPage{Transactions: p.transactions}, nil
}

type graphFixtureEvaluator struct {
	calls atomic.Int32
}

func (e *graphFixtureEvaluator) Evaluate(context.Context, analysis.EvaluationInput) (analysis.Evaluation, error) {
	e.calls.Add(1)
	score := 0
	return analysis.Evaluation{
		Score:           &score,
		Level:           "low",
		Reasons:         []string{},
		NodeAssessments: []analysis.NodeAssessment{},
		Source:          "rules-v1",
	}, nil
}

type graphPageResponse struct {
	DatasetID  string      `json:"datasetId"`
	Nodes      []graphNode `json:"nodes"`
	Edges      []graphEdge `json:"edges"`
	NextCursor *string     `json:"nextCursor"`
	HasMore    bool        `json:"hasMore"`
}

type graphNode struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Type    string `json:"type"`
}

type graphEdge struct {
	ID              string               `json:"id"`
	TransactionHash string               `json:"transactionHash"`
	EventIdentity   string               `json:"eventIdentity"`
	From            string               `json:"from"`
	To              string               `json:"to"`
	Amount          analysis.ExactAmount `json:"amount"`
	Timestamp       time.Time            `json:"timestamp"`
}

func TestGraphReturnsSavedTransferEventsWithoutReanalysis(t *testing.T) {
	fixture := newGraphHTTPFixture(t)
	test := fixture.test
	sharedHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	beforeResult := currentResultSnapshot(t, test, fixture.token, fixture.investigationID)
	providerCalls := fixture.provider.calls.Load()
	evaluatorCalls := fixture.evaluator.calls.Load()

	response := test.request(http.MethodGet, fixture.graphPath(""), nil, fixture.token)

	if response.Code != http.StatusOK {
		t.Fatalf("graph status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var page graphPageResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode graph page: %v; body: %s", err, response.Body.String())
	}
	if page.DatasetID != fixture.datasetID || page.HasMore || page.NextCursor != nil {
		t.Errorf("graph page boundary = %#v", page)
	}
	if len(page.Edges) != 4 {
		t.Fatalf("graph edges = %#v, want four unique Transfer events", page.Edges)
	}
	if page.Edges[0].ID != sharedHash+":log:0" || page.Edges[1].ID != sharedHash+":log:1" {
		t.Errorf("stable multi-event edge IDs = %#v", page.Edges[:2])
	}
	if page.Edges[0].TransactionHash != page.Edges[1].TransactionHash || page.Edges[0].EventIdentity == page.Edges[1].EventIdentity {
		t.Errorf("parent transaction/event identity was not preserved: %#v", page.Edges[:2])
	}
	if page.Edges[1].Amount.SmallestUnit != testLargeAmount || page.Edges[1].Amount.Decimals != 6 || page.Edges[1].Amount.Asset != analysis.AssetUSDT {
		t.Errorf("exact graph amount = %#v", page.Edges[1].Amount)
	}
	assertGraphNodes(t, page.Nodes, map[string]string{
		testTargetAddress: "focus",
		testPeerAddress:   "normal",
		testUSDTContract:  "normal",
	})
	var rawPage map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &rawPage); err != nil {
		t.Fatalf("decode raw graph page: %v", err)
	}
	for _, rawNode := range rawPage["nodes"].([]any) {
		node := rawNode.(map[string]any)
		for _, visualField := range []string{"x", "y", "group"} {
			if _, exists := node[visualField]; exists {
				t.Errorf("backend node includes visual field %q: %#v", visualField, node)
			}
		}
	}
	if fixture.provider.calls.Load() != providerCalls || fixture.evaluator.calls.Load() != evaluatorCalls {
		t.Errorf("graph GET invoked analysis: provider %d -> %d, evaluator %d -> %d", providerCalls, fixture.provider.calls.Load(), evaluatorCalls, fixture.evaluator.calls.Load())
	}
	afterResult := currentResultSnapshot(t, test, fixture.token, fixture.investigationID)
	if string(beforeResult) != string(afterResult) {
		t.Errorf("graph GET changed current result:\nbefore %s\nafter  %s", beforeResult, afterResult)
	}
	detail := test.request(http.MethodGet, "/api/v1/investigations/"+fixture.investigationID, nil, fixture.token)
	if detail.Code != http.StatusOK || !jsonBodyHasString(t, detail.Body.Bytes(), "status", "已完成") {
		t.Errorf("graph GET changed Investigation status: %d %s", detail.Code, detail.Body.String())
	}
}

func TestGraphCursorPaginatesInStableDatasetOrder(t *testing.T) {
	fixture := newGraphHTTPFixture(t)

	first := fixture.test.request(http.MethodGet, fixture.graphPath("&pageSize=2"), nil, fixture.token)
	repeated := fixture.test.request(http.MethodGet, fixture.graphPath("&pageSize=2"), nil, fixture.token)
	if first.Code != http.StatusOK {
		t.Fatalf("first graph page = %d %s", first.Code, first.Body.String())
	}
	if first.Body.String() != repeated.Body.String() {
		t.Errorf("repeated first page is unstable:\nfirst  %s\nrepeat %s", first.Body.String(), repeated.Body.String())
	}
	var firstPage graphPageResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode first graph page: %v", err)
	}
	if len(firstPage.Edges) != 2 || !firstPage.HasMore || firstPage.NextCursor == nil || *firstPage.NextCursor == "" {
		t.Fatalf("first graph boundary = %#v", firstPage)
	}
	if strings.Contains(*firstPage.NextCursor, fixture.datasetID) {
		t.Errorf("cursor exposes Dataset ID instead of remaining opaque: %q", *firstPage.NextCursor)
	}

	second := fixture.test.request(http.MethodGet, fixture.graphPath("&pageSize=2&cursor="+url.QueryEscape(*firstPage.NextCursor)), nil, fixture.token)
	if second.Code != http.StatusOK {
		t.Fatalf("second graph page = %d %s", second.Code, second.Body.String())
	}
	var secondPage graphPageResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second graph page: %v", err)
	}
	if len(secondPage.Edges) != 2 || secondPage.HasMore || secondPage.NextCursor != nil {
		t.Fatalf("second graph boundary = %#v", secondPage)
	}

	got := make([]string, 0, 4)
	seen := make(map[string]bool)
	for _, edge := range append(firstPage.Edges, secondPage.Edges...) {
		if seen[edge.ID] {
			t.Errorf("edge %q crossed a page boundary twice", edge.ID)
		}
		seen[edge.ID] = true
		got = append(got, edge.ID)
	}
	want := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:log:0",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:log:1",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb:log:0",
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc:log:0",
	}
	if !slices.Equal(got, want) {
		t.Errorf("paginated edge order = %#v, want %#v", got, want)
	}
	for _, page := range []graphPageResponse{firstPage, secondPage} {
		endpoints := make(map[string]string)
		for _, edge := range page.Edges {
			endpoints[edge.From] = "normal"
			endpoints[edge.To] = "normal"
		}
		if _, exists := endpoints[testTargetAddress]; exists {
			endpoints[testTargetAddress] = "focus"
		}
		assertGraphNodes(t, page.Nodes, endpoints)
	}
}

func TestGraphAnchorPaginatesOnlySavedAdjacentRelationships(t *testing.T) {
	fixture := newGraphHTTPFixture(t)
	providerCalls := fixture.provider.calls.Load()
	evaluatorCalls := fixture.evaluator.calls.Load()
	beforeResult := currentResultSnapshot(t, fixture.test, fixture.token, fixture.investigationID)
	anchorQuery := "&pageSize=2&anchor=" + url.QueryEscape(testPeerAddress)

	first := fixture.test.request(http.MethodGet, fixture.graphPath(anchorQuery), nil, fixture.token)
	if first.Code != http.StatusOK {
		t.Fatalf("first anchored graph page = %d %s", first.Code, first.Body.String())
	}
	var firstPage graphPageResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode first anchored graph page: %v", err)
	}
	if len(firstPage.Edges) != 2 || !firstPage.HasMore || firstPage.NextCursor == nil {
		t.Fatalf("first anchored boundary = %#v", firstPage)
	}
	assertEdgesTouchAnchor(t, firstPage, testPeerAddress)

	second := fixture.test.request(http.MethodGet, fixture.graphPath(anchorQuery+"&cursor="+url.QueryEscape(*firstPage.NextCursor)), nil, fixture.token)
	if second.Code != http.StatusOK {
		t.Fatalf("second anchored graph page = %d %s", second.Code, second.Body.String())
	}
	var secondPage graphPageResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second anchored graph page: %v", err)
	}
	if len(secondPage.Edges) != 1 || secondPage.HasMore || secondPage.NextCursor != nil {
		t.Fatalf("second anchored boundary = %#v", secondPage)
	}
	assertEdgesTouchAnchor(t, secondPage, testPeerAddress)

	wrongAnchor := fixture.test.request(http.MethodGet, fixture.graphPath("&pageSize=2&anchor="+url.QueryEscape(testTargetAddress)+"&cursor="+url.QueryEscape(*firstPage.NextCursor)), nil, fixture.token)
	assertErrorResponse(t, wrongAnchor.Code, wrongAnchor.Body.Bytes(), http.StatusBadRequest, "invalid_graph_cursor")
	empty := fixture.test.request(http.MethodGet, fixture.graphPath("&anchor=TUnknownSavedNode"), nil, fixture.token)
	if empty.Code != http.StatusOK {
		t.Fatalf("unknown anchor graph page = %d %s", empty.Code, empty.Body.String())
	}
	var emptyPage graphPageResponse
	if err := json.Unmarshal(empty.Body.Bytes(), &emptyPage); err != nil {
		t.Fatalf("decode unknown anchor graph page: %v", err)
	}
	if len(emptyPage.Edges) != 0 || len(emptyPage.Nodes) != 0 || emptyPage.HasMore || emptyPage.NextCursor != nil {
		t.Errorf("unknown anchor graph page = %#v", emptyPage)
	}
	if fixture.provider.calls.Load() != providerCalls || fixture.evaluator.calls.Load() != evaluatorCalls {
		t.Errorf("anchor expansion invoked analysis: provider %d -> %d, evaluator %d -> %d", providerCalls, fixture.provider.calls.Load(), evaluatorCalls, fixture.evaluator.calls.Load())
	}
	afterResult := currentResultSnapshot(t, fixture.test, fixture.token, fixture.investigationID)
	if string(beforeResult) != string(afterResult) {
		t.Errorf("anchor expansion changed current result:\nbefore %s\nafter  %s", beforeResult, afterResult)
	}
}

func TestGraphEnforcesOwnerCurrentDatasetAndStableQueryErrors(t *testing.T) {
	fixture := newGraphHTTPFixture(t)
	first := fixture.test.request(http.MethodGet, fixture.graphPath("&pageSize=1"), nil, fixture.token)
	if first.Code != http.StatusOK {
		t.Fatalf("cursor source page = %d %s", first.Code, first.Body.String())
	}
	var firstPage graphPageResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil || firstPage.NextCursor == nil {
		t.Fatalf("decode cursor source page: %#v, %v", firstPage, err)
	}

	invalidQueries := []string{
		"/api/v1/investigations/" + fixture.investigationID + "/graph",
		"/api/v1/investigations/" + fixture.investigationID + "/graph?datasetId=",
		fixture.graphPath("&pageSize=0"),
		fixture.graphPath("&pageSize=201"),
		fixture.graphPath("&pageSize=not-a-number"),
		fixture.graphPath("&anchor="),
		fixture.graphPath("&anchor=" + strings.Repeat("a", 65)),
	}
	for _, path := range invalidQueries {
		response := fixture.test.request(http.MethodGet, path, nil, fixture.token)
		assertErrorResponse(t, response.Code, response.Body.Bytes(), http.StatusBadRequest, "invalid_graph_query")
	}
	for _, cursor := range []string{"", "not-a-cursor", "%"} {
		response := fixture.test.request(http.MethodGet, fixture.graphPath("&cursor="+url.QueryEscape(cursor)), nil, fixture.token)
		assertErrorResponse(t, response.Code, response.Body.Bytes(), http.StatusBadRequest, "invalid_graph_cursor")
	}

	unauthenticated := fixture.test.request(http.MethodGet, fixture.graphPath(""), nil, "")
	assertErrorResponse(t, unauthenticated.Code, unauthenticated.Body.Bytes(), http.StatusUnauthorized, "unauthorized")
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	other := store.User{
		Username: "graph-other-" + suffix,
		Email:    "graph-other-" + suffix + "@auth-test.invalid",
		Password: "test-only",
		Salt:     "test-only",
	}
	if err := model.DB.Create(&other).Error; err != nil {
		t.Fatalf("create other graph owner: %v", err)
	}
	otherToken := ownerToken(t, other.ID)
	crossOwner := fixture.test.request(http.MethodGet, fixture.graphPath(""), nil, otherToken)
	missing := fixture.test.request(http.MethodGet, "/api/v1/investigations/missingInvestigation00000/graph?datasetId="+fixture.datasetID, nil, otherToken)
	assertErrorResponse(t, crossOwner.Code, crossOwner.Body.Bytes(), http.StatusNotFound, "investigation_not_found")
	assertErrorResponse(t, missing.Code, missing.Body.Bytes(), http.StatusNotFound, "investigation_not_found")
	if crossOwner.Body.String() != missing.Body.String() {
		t.Errorf("graph lookup enumerates ownership: cross=%s missing=%s", crossOwner.Body.String(), missing.Body.String())
	}

	oldDatasetID := fixture.datasetID
	currentDatasetID := completeGraphFixtureRun(t, fixture.test, fixture.token, fixture.investigationID)
	providerCalls := fixture.provider.calls.Load()
	evaluatorCalls := fixture.evaluator.calls.Load()
	stale := fixture.test.request(http.MethodGet, "/api/v1/investigations/"+fixture.investigationID+"/graph?datasetId="+oldDatasetID, nil, fixture.token)
	assertErrorResponse(t, stale.Code, stale.Body.Bytes(), http.StatusConflict, "stale_dataset")
	crossDatasetCursor := fixture.test.request(http.MethodGet,
		"/api/v1/investigations/"+fixture.investigationID+"/graph?datasetId="+currentDatasetID+"&cursor="+url.QueryEscape(*firstPage.NextCursor), nil, fixture.token)
	assertErrorResponse(t, crossDatasetCursor.Code, crossDatasetCursor.Body.Bytes(), http.StatusBadRequest, "invalid_graph_cursor")
	if fixture.provider.calls.Load() != providerCalls || fixture.evaluator.calls.Load() != evaluatorCalls {
		t.Errorf("invalid/stale graph reads invoked analysis: provider %d -> %d, evaluator %d -> %d", providerCalls, fixture.provider.calls.Load(), evaluatorCalls, fixture.evaluator.calls.Load())
	}
}

type graphHTTPFixture struct {
	test            *authHTTPTest
	token           string
	investigationID string
	datasetID       string
	provider        *graphFixtureProvider
	evaluator       *graphFixtureEvaluator
}

func newGraphHTTPFixture(t *testing.T) graphHTTPFixture {
	t.Helper()
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	provider := &graphFixtureProvider{
		cutoff: cutoff,
		transactions: []analysis.BlockchainTransaction{
			graphTransaction("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", cutoff.Add(-4*time.Hour),
				graphTransfer("log:1", testTargetAddress, testPeerAddress, testLargeAmount, cutoff.Add(-4*time.Hour+time.Second)),
				graphTransfer("log:0", testPeerAddress, testTargetAddress, "1", cutoff.Add(-4*time.Hour)),
				graphTransfer("log:0", testPeerAddress, testTargetAddress, "1", cutoff.Add(-4*time.Hour))),
			graphTransaction("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", cutoff.Add(-3*time.Hour),
				graphTransfer("log:0", testTargetAddress, testUSDTContract, "42", cutoff.Add(-3*time.Hour))),
			graphTransaction("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", cutoff.Add(-2*time.Hour),
				graphTransfer("log:0", testTargetAddress, testPeerAddress, "7", cutoff.Add(-2*time.Hour))),
		},
	}
	evaluator := &graphFixtureEvaluator{}
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: provider, Evaluator: evaluator})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)
	datasetID := completeGraphFixtureRun(t, test, token, investigationID)
	return graphHTTPFixture{
		test:            test,
		token:           token,
		investigationID: investigationID,
		datasetID:       datasetID,
		provider:        provider,
		evaluator:       evaluator,
	}
}

func (f graphHTTPFixture) graphPath(suffix string) string {
	return "/api/v1/investigations/" + f.investigationID + "/graph?datasetId=" + f.datasetID + suffix
}

func graphTransaction(hash string, timestamp time.Time, transfers ...analysis.TRC20Transfer) analysis.BlockchainTransaction {
	return analysis.BlockchainTransaction{
		Network:        "TRON_MAINNET",
		Hash:           hash,
		BlockID:        "block-" + hash[:8],
		BlockTimestamp: timestamp,
		Successful:     true,
		Confirmed:      true,
		Transfers:      transfers,
	}
}

func graphTransfer(eventIdentity, from, to, amount string, timestamp time.Time) analysis.TRC20Transfer {
	return analysis.TRC20Transfer{
		EventIdentity:      eventIdentity,
		ContractAddress:    testUSDTContract,
		Asset:              analysis.AssetUSDT,
		FromAddress:        from,
		ToAddress:          to,
		AmountSmallestUnit: amount,
		Decimals:           6,
		Timestamp:          timestamp,
	}
}

func completeGraphFixtureRun(t *testing.T, test *authHTTPTest, token, investigationID string) string {
	t.Helper()
	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if started.Code != http.StatusAccepted {
		t.Fatalf("start graph fixture run = %d %s", started.Code, started.Body.String())
	}
	completed := pollAnalysisRun(t, test, token, investigationID, decodeRunID(t, started.Body.Bytes()))
	if completed.Status != "completed" || completed.ResultID == nil {
		t.Fatalf("graph fixture run = %#v", completed)
	}
	return *completed.ResultID
}

func currentResultSnapshot(t *testing.T, test *authHTTPTest, token, investigationID string) json.RawMessage {
	t.Helper()
	response := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/current-result", nil, token)
	if response.Code != http.StatusOK {
		t.Fatalf("current result snapshot = %d %s", response.Code, response.Body.String())
	}
	var result json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode current result snapshot: %v", err)
	}
	return result
}

func assertGraphNodes(t *testing.T, nodes []graphNode, want map[string]string) {
	t.Helper()
	if len(nodes) != len(want) {
		t.Fatalf("graph nodes = %#v, want %d unique endpoint nodes", nodes, len(want))
	}
	seen := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		if node.ID != node.Address || want[node.Address] != node.Type || seen[node.ID] {
			t.Errorf("graph node = %#v, want id=address and type %q", node, want[node.Address])
		}
		seen[node.ID] = true
	}
}

func assertEdgesTouchAnchor(t *testing.T, page graphPageResponse, anchor string) {
	t.Helper()
	endpoints := make(map[string]string)
	for _, edge := range page.Edges {
		if edge.From != anchor && edge.To != anchor {
			t.Errorf("anchored edge does not touch %q: %#v", anchor, edge)
		}
		endpoints[edge.From] = "normal"
		endpoints[edge.To] = "normal"
	}
	if _, exists := endpoints[testTargetAddress]; exists {
		endpoints[testTargetAddress] = "focus"
	}
	assertGraphNodes(t, page.Nodes, endpoints)
}

func assertErrorResponse(t *testing.T, status int, body []byte, wantStatus int, wantCode string) {
	t.Helper()
	if status != wantStatus {
		t.Fatalf("error status = %d, want %d; body: %s", status, wantStatus, body)
	}
	if !jsonBodyHasString(t, body, "code", wantCode) {
		t.Errorf("error body = %s, want code %q", body, wantCode)
	}
}

func jsonBodyHasString(t *testing.T, body []byte, field, want string) bool {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode JSON body: %v", err)
	}
	return value[field] == want
}
