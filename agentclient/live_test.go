package agentclient

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveAgentContract drives a running Python sidecar to prove the two sides
// agree on the wire format. Everything else in this package uses a scripted
// agent; only this test can catch a field name that both sides spell
// differently, because that failure is invisible until runtime.
//
// It is skipped unless pointed at a live sidecar, since it needs the Agent
// process, the network, and real tokens:
//
//	CHAINTRACE_LIVE_AGENT_URL=http://127.0.0.1:7795 \
//	CHAINTRACE_LIVE_AGENT_KEY=... \
//	go test ./agentclient/ -run TestLiveAgentContract -v
func TestLiveAgentContract(t *testing.T) {
	baseURL := os.Getenv("CHAINTRACE_LIVE_AGENT_URL")
	sharedKey := os.Getenv("CHAINTRACE_LIVE_AGENT_KEY")
	if baseURL == "" || sharedKey == "" {
		t.Skip("set CHAINTRACE_LIVE_AGENT_URL and CHAINTRACE_LIVE_AGENT_KEY to run")
	}

	provider := New(Options{
		BaseURL:     baseURL,
		SharedKey:   sharedKey,
		HTTPTimeout: 120 * time.Second,
		TurnTimeout: 240 * time.Second,
	}, nil)
	if provider == nil {
		t.Fatal("provider disabled despite configuration")
	}

	scope := testScope()
	evidence := Evidence{
		Investigation: describeInvestigation(scope.investigation),
		Analysis:      buildAnalysisEvidence(testCurrentResult(), targetAddress, scope.view),
	}
	opening := chatRequest{
		SessionID: "live-contract-test",
		DatasetID: scope.datasetID,
		Mode:      "chat",
		Evidence:  &evidence,
		Messages: []wireMessage{{
			Role:    "user",
			Content: "請用 get_address_detail 查 " + targetAddress + "，然後告訴我它轉出給幾個不同地址。",
		}},
		Tools: ToolSpecs(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	text, err := provider.runTurn(ctx, scope, opening)
	if err != nil {
		t.Fatalf("live turn failed: %v", err)
	}
	t.Logf("agent answered: %s", text)

	if strings.TrimSpace(text) == "" {
		t.Fatal("live turn produced no text")
	}
	// The dataset says the target paid two distinct addresses. The Agent can
	// only know that by calling a tool and reading the result we sent back, so
	// this doubles as proof the tool round trip works end to end.
	if !strings.Contains(text, "2") && !strings.Contains(text, "兩") {
		t.Errorf("answer does not reflect the tool result: %s", text)
	}
}
