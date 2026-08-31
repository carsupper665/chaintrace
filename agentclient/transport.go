package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// errSessionExpired means the sidecar no longer holds the turn's context. It is
// not a failure: the sidecar's session is a cache, and the Go database is the
// source of truth, so the caller simply resends the full payload.
var errSessionExpired = errors.New("agent session expired")

// maximumAgentResponse bounds what a sidecar can make this process allocate.
const maximumAgentResponse = 4 << 20

type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	SessionID   string        `json:"session_id"`
	DatasetID   string        `json:"dataset_id"`
	Mode        string        `json:"mode"`
	Evidence    *Evidence     `json:"evidence,omitempty"`
	Messages    []wireMessage `json:"messages,omitempty"`
	Tools       []ToolSpec    `json:"tools,omitempty"`
	ToolResults []ToolResult  `json:"tool_results,omitempty"`
}

type chatUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type chatResponse struct {
	Text       string     `json:"text"`
	ToolCalls  []ToolCall `json:"tool_calls"`
	StopReason string     `json:"stop_reason"`
	Usage      chatUsage  `json:"usage"`
}

type agentError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// transport is the only thing in this package that speaks HTTP to the sidecar.
type transport struct {
	baseURL   string
	sharedKey string
	client    *http.Client
}

func newTransport(options Options) *transport {
	return &transport{
		baseURL:   strings.TrimRight(options.BaseURL, "/"),
		sharedKey: options.SharedKey,
		client:    &http.Client{Timeout: options.HTTPTimeout},
	}
}

func (t *transport) chat(ctx context.Context, payload chatRequest) (chatResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return chatResponse{}, err
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, t.baseURL+"/v1/agent/chat", bytes.NewReader(body),
	)
	if err != nil {
		return chatResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Agent-Key", t.sharedKey)

	response, err := t.client.Do(request)
	if err != nil {
		return chatResponse{}, err
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumAgentResponse))
	if err != nil {
		return chatResponse{}, err
	}
	return decodeChatResponse(response.StatusCode, raw)
}

func decodeChatResponse(status int, raw []byte) (chatResponse, error) {
	if status == http.StatusOK {
		var reply chatResponse
		if err := json.Unmarshal(raw, &reply); err != nil {
			return chatResponse{}, fmt.Errorf("agent returned malformed JSON: %w", err)
		}
		return reply, nil
	}

	var failure agentError
	if json.Unmarshal(raw, &failure) != nil || failure.Code == "" {
		return chatResponse{}, fmt.Errorf("agent returned %d", status)
	}
	if status == http.StatusConflict && failure.Code == "session_expired" {
		return chatResponse{}, errSessionExpired
	}
	return chatResponse{}, fmt.Errorf("agent %s: %s", failure.Code, failure.Message)
}
