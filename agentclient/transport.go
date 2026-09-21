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

	"chaintrace/controller"
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
	// Model picks one of the sidecar's configured compatible models. Empty
	// means the sidecar's default provider.
	Model string `json:"model,omitempty"`
}

type modelsResponse struct {
	Models []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"models"`
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

// models asks the sidecar which compatible models it can run. Tokens never
// leave the sidecar; this is only ids and display names.
func (t *transport) models(ctx context.Context) ([]controller.AgentModel, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Agent-Key", t.sharedKey)

	response, err := t.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumAgentResponse))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent returned %d", response.StatusCode)
	}
	var decoded modelsResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("agent returned malformed JSON: %w", err)
	}
	models := make([]controller.AgentModel, 0, len(decoded.Models))
	for _, m := range decoded.Models {
		models = append(models, controller.AgentModel{ID: m.ID, DisplayName: m.DisplayName})
	}
	return models, nil
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
