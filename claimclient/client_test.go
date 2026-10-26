package claimclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestExtractPNGPostsBase64ImageWithSharedKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/extract-claims" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Agent-Key") != "secret" {
			t.Fatalf("missing shared key")
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload map[string]string
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["mime_type"] != "image/png" {
			t.Fatalf("mime = %q", payload["mime_type"])
		}
		decoded, err := base64.StdEncoding.DecodeString(payload["image_base64"])
		if err != nil || string(decoded) != string(tinyPNG) {
			t.Fatalf("image round-trip failed: %v", err)
		}
		_ = json.NewEncoder(w).Encode(ExtractResult{
			Network: "tron",
			Claims: []AddressClaim{{
				Kind: "address", Value: "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj",
				Role: "to", Confidence: 0.9,
			}},
		})
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	result, err := client.ExtractPNG(context.Background(), tinyPNG)
	if err != nil {
		t.Fatalf("ExtractPNG: %v", err)
	}
	if result.Network != "tron" || len(result.Claims) != 1 || result.Claims[0].Value != "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExtractPNGSurfacesAgentErrorCodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"invalid_claim_address","message":"bad"}`))
	}))
	defer server.Close()

	_, err := New(server.URL, "secret").ExtractPNG(context.Background(), tinyPNG)
	var extractErr *ExtractError
	if err == nil || !errors.As(err, &extractErr) || extractErr.Code != "invalid_claim_address" {
		t.Fatalf("err = %v", err)
	}
}

func TestNewReturnsNilWhenNotConfigured(t *testing.T) {
	if New("", "key") != nil || New("http://localhost", "") != nil {
		t.Fatal("expected nil client when incomplete")
	}
}
