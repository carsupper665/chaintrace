package agentclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeAgent stands in for the Python sidecar. Scripted replies let the loop be
// driven without a model, a network, or an API key.
type fakeAgent struct {
	replies  []chatResponse
	statuses []int
	requests []chatRequest
	keys     []string
	server   *httptest.Server
}

func newFakeAgent(t *testing.T, replies ...chatResponse) *fakeAgent {
	t.Helper()
	agent := &fakeAgent{replies: replies}
	agent.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload chatRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode agent request: %v", err)
		}
		agent.requests = append(agent.requests, payload)
		agent.keys = append(agent.keys, r.Header.Get("X-Agent-Key"))

		index := len(agent.requests) - 1
		if index < len(agent.statuses) && agent.statuses[index] != http.StatusOK {
			w.WriteHeader(agent.statuses[index])
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "session_expired"})
			return
		}
		if index >= len(agent.replies) {
			t.Errorf("agent called %d times, only %d replies scripted", index+1, len(agent.replies))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(agent.replies[index])
	}))
	t.Cleanup(agent.server.Close)
	return agent
}

func (a *fakeAgent) provider(t *testing.T, options Options) *Provider {
	t.Helper()
	options.BaseURL = a.server.URL
	options.SharedKey = "test-key"
	provider := New(options, nil)
	if provider == nil {
		t.Fatal("provider was disabled despite a base URL and shared key")
	}
	return provider
}

func openingRequest() chatRequest {
	analysisEvidence := testEvidence()
	evidence := Evidence{
		Investigation: describeInvestigation(testInvestigation()),
		Analysis:      &analysisEvidence,
	}
	return chatRequest{
		SessionID: "inv1", DatasetID: "ds1", Mode: "chat",
		Evidence: &evidence,
		Messages: []wireMessage{{Role: "user", Content: "這個地址有什麼異常?"}},
		Tools:    ToolSpecs(),
	}
}

func toolCallReply(name string, arguments map[string]any) chatResponse {
	return chatResponse{
		StopReason: "tool_calls",
		ToolCalls:  []ToolCall{{ID: "fc_1", Name: name, Arguments: arguments}},
	}
}

func TestDisabledWithoutBaseURLOrSharedKey(t *testing.T) {
	// An unauthenticated sidecar would accept anything on the LAN, so a missing
	// shared key disables the Agent just like a missing URL.
	cases := map[string]Options{
		"neither":     {},
		"noSharedKey": {BaseURL: "http://127.0.0.1:7795"},
		"noBaseURL":   {SharedKey: "k"},
	}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			if New(options, nil) != nil {
				t.Error("provider was created without complete configuration")
			}
		})
	}
}

func TestTurnSendsSharedKeyAndEvidence(t *testing.T) {
	agent := newFakeAgent(t, chatResponse{Text: "沒有異常", StopReason: "stop"})
	provider := agent.provider(t, Options{})

	text, err := provider.runTurn(context.Background(), testScope(), openingRequest())
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if text != "沒有異常" {
		t.Errorf("text = %q", text)
	}
	if agent.keys[0] != "test-key" {
		t.Errorf("X-Agent-Key = %q", agent.keys[0])
	}
	if agent.requests[0].Evidence == nil || agent.requests[0].Evidence.Analysis.Dataset.ID != "ds1" {
		t.Error("evidence was not sent on the opening call")
	}
	if len(agent.requests[0].Tools) == 0 {
		t.Error("tool contract was not sent")
	}
}

func TestToolCallIsExecutedAndFedBack(t *testing.T) {
	agent := newFakeAgent(t,
		toolCallReply(ToolGetAddressDetail, map[string]any{"address": targetAddress}),
		chatResponse{Text: "它轉出給 2 個地址", StopReason: "stop"},
	)
	provider := agent.provider(t, Options{})

	text, err := provider.runTurn(context.Background(), testScope(), openingRequest())
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if text != "它轉出給 2 個地址" {
		t.Errorf("text = %q", text)
	}
	if len(agent.requests) != 2 {
		t.Fatalf("agent calls = %d, want 2", len(agent.requests))
	}
	results := agent.requests[1].ToolResults
	if len(results) != 1 || results[0].CallID != "fc_1" || results[0].IsError {
		t.Fatalf("tool results = %+v", results)
	}
	if !strings.Contains(results[0].Content, "150 USDT") {
		t.Errorf("tool result did not carry the computed totals: %s", results[0].Content)
	}
	// Continuations must not resend the evidence; the sidecar caches it.
	if agent.requests[1].Evidence != nil {
		t.Error("evidence was resent on a continuation")
	}
}

// A refused tool must still reach the Agent so it can tell the analyst.
func TestOutOfScopeRefusalIsSentBackToTheAgent(t *testing.T) {
	agent := newFakeAgent(t,
		toolCallReply(ToolExpandNode, map[string]any{"address": unknownAddress}),
		chatResponse{Text: "該地址不在本次分析範圍內", StopReason: "stop"},
	)
	provider := agent.provider(t, Options{})

	if _, err := provider.runTurn(context.Background(), testScope(), openingRequest()); err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	results := agent.requests[1].ToolResults
	if len(results) != 1 || !results[0].IsError {
		t.Fatalf("tool results = %+v", results)
	}
	if !strings.Contains(results[0].Content, codeOutOfScope) {
		t.Errorf("refusal did not carry its code: %s", results[0].Content)
	}
}

// Check 4a: a model that keeps asking for tools cannot loop forever.
func TestToolCallsAreCapped(t *testing.T) {
	looping := make([]chatResponse, 0, 8)
	for range 8 {
		looping = append(looping, toolCallReply(ToolGetAddressDetail, map[string]any{"address": targetAddress}))
	}
	agent := newFakeAgent(t, looping...)
	provider := agent.provider(t, Options{MaxToolCalls: 3})

	_, err := provider.runTurn(context.Background(), testScope(), openingRequest())
	if err == nil {
		t.Fatal("a turn that never produced text should report an error, not an empty answer")
	}
	// One opening call plus three tool round trips.
	if len(agent.requests) != 4 {
		t.Errorf("agent calls = %d, want 4", len(agent.requests))
	}
}

func TestTurnStopsAtTheDeadline(t *testing.T) {
	agent := newFakeAgent(t,
		toolCallReply(ToolGetAddressDetail, map[string]any{"address": targetAddress}),
		chatResponse{Text: "來不及了", StopReason: "stop"},
	)
	provider := agent.provider(t, Options{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.runTurn(ctx, testScope(), openingRequest()); err == nil {
		t.Fatal("a cancelled turn should fail rather than answer")
	}
}

// The sidecar's session is a cache. Losing it is recoverable, not fatal.
func TestSessionExpiryRestartsTheTurnOnce(t *testing.T) {
	agent := newFakeAgent(t,
		toolCallReply(ToolGetAddressDetail, map[string]any{"address": targetAddress}),
		chatResponse{}, // 409, scripted below
		chatResponse{Text: "重建後的回答", StopReason: "stop"},
	)
	agent.statuses = []int{http.StatusOK, http.StatusConflict, http.StatusOK}
	provider := agent.provider(t, Options{})

	text, err := provider.runTurn(context.Background(), testScope(), openingRequest())
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if text != "重建後的回答" {
		t.Errorf("text = %q", text)
	}
	if len(agent.requests) != 3 {
		t.Fatalf("agent calls = %d, want 3", len(agent.requests))
	}
	// The retry resends the full context, which is the whole point.
	if agent.requests[2].Evidence == nil || len(agent.requests[2].Messages) == 0 {
		t.Error("the retry did not resend evidence and history")
	}
}

func TestAgentFailureIsReportedNotFaked(t *testing.T) {
	agent := newFakeAgent(t)
	agent.statuses = []int{http.StatusBadGateway}
	provider := agent.provider(t, Options{})

	if _, err := provider.runTurn(context.Background(), testScope(), openingRequest()); err == nil {
		t.Fatal("a failing sidecar must surface an error, never an invented answer")
	}
}

func TestEmptyAnswerIsAnError(t *testing.T) {
	agent := newFakeAgent(t, chatResponse{Text: "", StopReason: "stop"})
	provider := agent.provider(t, Options{})

	if _, err := provider.runTurn(context.Background(), testScope(), openingRequest()); err == nil {
		t.Fatal("a blank completion must not be stored as an agent message")
	}
}

func TestOptionsDefaultsFillEveryBound(t *testing.T) {
	options := Options{BaseURL: "http://x", SharedKey: "k"}.withDefaults()
	if options.MaxToolCalls <= 0 || options.ToolMaxRows <= 0 ||
		options.TurnTimeout <= 0 || options.HTTPTimeout <= 0 {
		t.Errorf("a zero-valued Options left a limit disabled: %+v", options)
	}
	if options.TurnTimeout < options.HTTPTimeout {
		t.Errorf("turn budget %v is shorter than one call's timeout %v",
			options.TurnTimeout, options.HTTPTimeout)
	}
}

func TestLRUCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newLRUCache[string](2)
	cache.Put("a", "1")
	cache.Put("b", "2")
	if _, ok := cache.Get("a"); !ok { // "a" becomes the most recent
		t.Fatal("a missing right after insertion")
	}
	cache.Put("c", "3")
	if _, ok := cache.Get("b"); ok {
		t.Error("b should have been evicted")
	}
	for _, key := range []string{"a", "c"} {
		if _, ok := cache.Get(key); !ok {
			t.Errorf("%s should have survived", key)
		}
	}
	if cache.Len() != 2 {
		t.Errorf("len = %d, want 2", cache.Len())
	}
}

func TestLRUCacheOverwritesWithoutGrowing(t *testing.T) {
	cache := newLRUCache[int](4)
	cache.Put("k", 1)
	cache.Put("k", 2)
	value, ok := cache.Get("k")
	if !ok || value != 2 {
		t.Errorf("value = %d, %v", value, ok)
	}
	if cache.Len() != 1 {
		t.Errorf("len = %d, want 1", cache.Len())
	}
}

func TestTransportTimeoutIsApplied(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(chatResponse{Text: "too late"})
	}))
	t.Cleanup(slow.Close)

	client := newTransport(Options{
		BaseURL: slow.URL, SharedKey: "k", HTTPTimeout: 20 * time.Millisecond,
	})
	if _, err := client.chat(context.Background(), openingRequest()); err == nil {
		t.Fatal("a slow sidecar should time out")
	}
}
