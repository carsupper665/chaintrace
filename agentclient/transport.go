package agentclient

import (
	"bufio"
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
	// Stream is only set by chatStream, which always posts to
	// /v1/agent/chat/stream; chat leaves it false and posts to /v1/agent/chat.
	Stream bool `json:"stream,omitempty"`
}

// streamEvent is one SSE frame from /v1/agent/chat/stream. Only the fields
// that frame kind uses are populated; the rest stay zero.
type streamEvent struct {
	Type       string     `json:"type"`
	Text       string     `json:"text"`
	ToolCalls  []ToolCall `json:"tool_calls"`
	StopReason string     `json:"stop_reason"`
	Usage      chatUsage  `json:"usage"`
	Code       string     `json:"code"`
	Message    string     `json:"message"`
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

// chatStream is chat's streaming twin: it posts to /v1/agent/chat/stream and
// calls onChunk for every text/thinking delta as it arrives, instead of
// buffering the whole body. It still returns the same chatResponse chat does,
// built from the stream's terminal "done" frame, so callers that only care
// about the final answer (tool-call continuations, persistence) don't need to
// change. onChunk's kind is "text" or "thinking".
func (t *transport) chatStream(
	ctx context.Context, payload chatRequest, onChunk func(kind, text string) error,
) (chatResponse, error) {
	payload.Stream = true
	body, err := json.Marshal(payload)
	if err != nil {
		return chatResponse{}, err
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, t.baseURL+"/v1/agent/chat/stream", bytes.NewReader(body),
	)
	if err != nil {
		return chatResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("X-Agent-Key", t.sharedKey)

	response, err := t.client.Do(request)
	if err != nil {
		return chatResponse{}, err
	}
	defer response.Body.Close()

	// The sidecar can still reject the request outright (bad key, bad JSON)
	// before it ever starts streaming; that comes back as an ordinary JSON
	// error body, not SSE frames.
	if response.StatusCode != http.StatusOK {
		raw, err := io.ReadAll(io.LimitReader(response.Body, maximumAgentResponse))
		if err != nil {
			return chatResponse{}, err
		}
		return decodeChatResponse(response.StatusCode, raw)
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maximumAgentResponse)
	for scanner.Scan() {
		line := scanner.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue // blank line between frames, or anything else non-SSE
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return chatResponse{}, fmt.Errorf("agent returned malformed JSON: %w", err)
		}
		switch event.Type {
		case "text_delta":
			if err := onChunk("text", event.Text); err != nil {
				return chatResponse{}, err
			}
		case "thinking_delta":
			if err := onChunk("thinking", event.Text); err != nil {
				return chatResponse{}, err
			}
		case "done":
			return chatResponse{
				Text: event.Text, ToolCalls: event.ToolCalls,
				StopReason: event.StopReason, Usage: event.Usage,
			}, nil
		case "error":
			if event.Code == "session_expired" {
				return chatResponse{}, errSessionExpired
			}
			return chatResponse{}, fmt.Errorf("agent %s: %s", event.Code, event.Message)
		}
	}
	if err := scanner.Err(); err != nil {
		return chatResponse{}, err
	}
	return chatResponse{}, fmt.Errorf("agent stream ended without a done frame")
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
