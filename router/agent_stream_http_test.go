package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"chaintrace/controller"
)

// streamingAgent also streams text/thinking deltas, like the real
// sidecar-backed provider. Respond (non-streaming) still works via the
// embedded scriptedAgent, unused by these tests.
type streamingAgent struct {
	scriptedAgent
	chunks []streamChunk
}

type streamChunk struct {
	kind, text string
}

func (a *streamingAgent) RespondStream(
	_ context.Context, request controller.AgentRequest, onChunk func(kind, text string) error,
) (controller.AgentResponse, error) {
	a.requests = append(a.requests, request)
	for _, chunk := range a.chunks {
		if err := onChunk(chunk.kind, chunk.text); err != nil {
			return controller.AgentResponse{}, err
		}
	}
	if a.err != nil {
		return controller.AgentResponse{}, a.err
	}
	return controller.AgentResponse{Content: a.answer}, nil
}

func parseSSEFrames(t *testing.T, body string) []map[string]any {
	t.Helper()
	var frames []map[string]any
	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		data := strings.TrimPrefix(block, "data: ")
		var frame map[string]any
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			t.Fatalf("parse SSE frame %q: %v", block, err)
		}
		frames = append(frames, frame)
	}
	return frames
}

func TestConversationStreamEmitsDeltasThenDone(t *testing.T) {
	agent := &streamingAgent{
		scriptedAgent: scriptedAgent{answer: "這個地址轉出給 14 個不同地址。"},
		chunks: []streamChunk{
			{kind: "thinking", text: "想一下"},
			{kind: "text", text: "這個地址"},
			{kind: "text", text: "轉出給 14 個不同地址。"},
		},
	}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "Streaming agent")

	response := test.request(http.MethodPost,
		"/api/v1/investigations/"+investigationID+"/conversation/stream",
		map[string]string{"idempotencyKey": "cmd-1", "message": "有什麼異常?"}, token)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if ct := response.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type = %q", ct)
	}
	frames := parseSSEFrames(t, response.Body.String())
	if len(frames) != 4 {
		t.Fatalf("frames = %#v, want 3 deltas + done", frames)
	}
	if frames[0]["type"] != "thinking_delta" || frames[0]["text"] != "想一下" {
		t.Errorf("frame[0] = %#v", frames[0])
	}
	if frames[1]["type"] != "text_delta" || frames[1]["text"] != "這個地址" {
		t.Errorf("frame[1] = %#v", frames[1])
	}
	last := frames[3]
	if last["type"] != "done" {
		t.Fatalf("last frame = %#v, want done", last)
	}
	messages, ok := last["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("done messages = %#v, want question + answer", last["messages"])
	}
}

// A failure before any chunk was sent still degrades exactly like the
// non-streaming route: a normal 503 JSON body, not an SSE frame.
func TestConversationStreamDegradesBeforeAnyChunk(t *testing.T) {
	agent := &streamingAgent{scriptedAgent: scriptedAgent{err: context.DeadlineExceeded}}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "Fails before streaming")

	response := test.request(http.MethodPost,
		"/api/v1/investigations/"+investigationID+"/conversation/stream",
		map[string]string{"idempotencyKey": "cmd-1", "message": "有什麼異常?"}, token)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body: %s",
			response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	var got struct {
		Code      string `json:"code"`
		Persisted bool   `json:"persisted"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Code != "agent_unavailable" || !got.Persisted {
		t.Errorf("response = %#v", got)
	}
}

// Once headers have gone out, a failure can only end the stream with a
// terminal error frame — the status is already committed to 200.
func TestConversationStreamDegradesAfterChunksWereSent(t *testing.T) {
	agent := &streamingAgent{
		scriptedAgent: scriptedAgent{err: context.DeadlineExceeded},
		chunks:        []streamChunk{{kind: "text", text: "查到一半"}},
	}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "Fails mid-stream")

	response := test.request(http.MethodPost,
		"/api/v1/investigations/"+investigationID+"/conversation/stream",
		map[string]string{"idempotencyKey": "cmd-1", "message": "有什麼異常?"}, token)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (headers already sent); body: %s",
			response.Code, http.StatusOK, response.Body.String())
	}
	frames := parseSSEFrames(t, response.Body.String())
	if len(frames) != 2 {
		t.Fatalf("frames = %#v, want the delta + a terminal error frame", frames)
	}
	last := frames[1]
	if last["type"] != "error" || last["code"] != "agent_unavailable" {
		t.Fatalf("last frame = %#v", last)
	}
	// The question plus the synthetic system outcome, same as the
	// non-streaming route's writeUnavailable.
	messages, ok := last["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("error messages = %#v, want the question + system outcome", last["messages"])
	}
}

// A provider that cannot stream degrades like a missing Agent.
func TestConversationStreamUnavailableWithoutAStreamer(t *testing.T) {
	test, token := newAgentTest(t, &scriptedAgent{answer: "ok"})
	investigationID := createConversationInvestigation(t, test, token, "No streamer")

	response := test.request(http.MethodPost,
		"/api/v1/investigations/"+investigationID+"/conversation/stream",
		map[string]string{"idempotencyKey": "cmd-1", "message": "hi"}, token)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body: %s",
			response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}

func TestConversationStreamPassesTheChosenModel(t *testing.T) {
	agent := &streamingAgent{scriptedAgent: scriptedAgent{answer: "ok"}}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "Streamed model pick")

	test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/conversation/stream",
		map[string]string{"idempotencyKey": "cmd-1", "message": "hi", "model": "wire-id"}, token)

	if len(agent.requests) != 1 || agent.requests[0].Model != "wire-id" {
		t.Fatalf("provider requests = %+v", agent.requests)
	}
}
