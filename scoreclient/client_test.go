package scoreclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"chaintrace/analysis"
)

func testInput() analysis.LearnedScoreInput {
	return analysis.LearnedScoreInput{
		TargetAddress: "TTarget",
		Network:       "TRON_MAINNET",
		Asset:         "USDT",
		WindowStart:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		WindowEnd:     time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Truncated:     false,
		Decimals:      6,
		Transfers: []analysis.TRC20Transfer{
			{
				FromAddress: "TFrom",
				ToAddress:   "TTarget",
				// 比 2^53 (9007199254740992) 多 1：字串形式必須逐字元保留，
				// 這是 features.py 那條「金額全程用 int」規則的線上對應。
				AmountSmallestUnit: "9007199254740993",
				Timestamp:          time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
			},
		},
	}
}

func TestScoreRoundTrip(t *testing.T) {
	var gotRequest scoreRequestWire
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Agent-Key")
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(scoreResponseWire{Score: -0.42, ModelVersion: "abc123"})
	}))
	defer server.Close()

	client := New(Options{BaseURL: server.URL, SharedKey: "secret"})
	if client == nil {
		t.Fatal("client disabled despite configuration")
	}
	result, err := client.Score(context.Background(), testInput())
	if err != nil {
		t.Fatalf("Score() error = %v", err)
	}
	if result.Score != -0.42 || result.Source != "abc123" {
		t.Errorf("result = %+v, want {Score:-0.42 Source:abc123}", result)
	}
	if gotKey != "secret" {
		t.Errorf("X-Agent-Key = %q, want secret", gotKey)
	}
	if gotRequest.TargetAddress != "TTarget" {
		t.Errorf("target_address = %q, want TTarget", gotRequest.TargetAddress)
	}
	if gotRequest.Transfers[0].Amount != "9007199254740993" {
		t.Errorf("amount = %q, precision lost over the wire", gotRequest.Transfers[0].Amount)
	}
}

func TestScoreNon200IsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(scoreErrorWire{Code: "unauthorized", Message: "Invalid agent key"})
	}))
	defer server.Close()

	client := New(Options{BaseURL: server.URL, SharedKey: "wrong"})
	_, err := client.Score(context.Background(), testInput())
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("Score() error = %v, want it to mention unauthorized", err)
	}
}

func TestScoreMalformedJSONIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := New(Options{BaseURL: server.URL, SharedKey: "secret"})
	if _, err := client.Score(context.Background(), testInput()); err == nil {
		t.Error("Score() error = nil, want an error for malformed JSON")
	}
}

func TestScoreTimeoutIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(scoreResponseWire{Score: 0, ModelVersion: "abc"})
	}))
	defer server.Close()

	client := New(Options{BaseURL: server.URL, SharedKey: "secret", HTTPTimeout: 5 * time.Millisecond})
	if _, err := client.Score(context.Background(), testInput()); err == nil {
		t.Error("Score() error = nil, want a timeout error")
	}
}
