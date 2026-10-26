// Package claimclient calls the Python Agent sidecar's POST /v1/extract-claims
// route. It mirrors scoreclient: one request, one response, separate from the
// chat/tool-loop agentclient package.
package claimclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	maximumExtractResponse = 64 << 10
	extractTimeout         = 30 * time.Second
)

// AddressClaim is one unverified structured assertion from a screenshot.
type AddressClaim struct {
	Kind       string  `json:"kind"`
	Value      string  `json:"value"`
	Role       string  `json:"role,omitempty"`
	Asset      string  `json:"asset,omitempty"`
	Confidence float64 `json:"confidence"`
}

// ExtractResult is the Agent's structured extract reply after its own validation.
type ExtractResult struct {
	Network string         `json:"network"`
	Claims  []AddressClaim `json:"claims"`
}

type extractRequestWire struct {
	ImageBase64 string `json:"image_base64"`
	MimeType    string `json:"mime_type"`
}

type extractErrorWire struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ExtractError is a machine-readable failure from the sidecar extract route.
type ExtractError struct {
	Code    string
	Message string
	Status  int
}

func (e *ExtractError) Error() string {
	return fmt.Sprintf("extract %s: %s", e.Code, e.Message)
}

// Client extracts Claims over HTTP.
type Client struct {
	baseURL   string
	sharedKey string
	client    *http.Client
}

// New returns nil when the sidecar is not configured, matching scoreclient.
func New(baseURL, sharedKey string) *Client {
	if baseURL == "" || sharedKey == "" {
		return nil
	}
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		sharedKey: sharedKey,
		client:    &http.Client{Timeout: extractTimeout},
	}
}

// ExtractPNG sends PNG bytes to the Agent and returns one pending address Claim.
func (c *Client) ExtractPNG(ctx context.Context, image []byte) (ExtractResult, error) {
	body, err := json.Marshal(extractRequestWire{
		ImageBase64: base64.StdEncoding.EncodeToString(image),
		MimeType:    "image/png",
	})
	if err != nil {
		return ExtractResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/extract-claims", bytes.NewReader(body))
	if err != nil {
		return ExtractResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Agent-Key", c.sharedKey)

	response, err := c.client.Do(request)
	if err != nil {
		return ExtractResult{}, err
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumExtractResponse))
	if err != nil {
		return ExtractResult{}, err
	}
	return decodeExtractResponse(response.StatusCode, raw)
}

func decodeExtractResponse(status int, raw []byte) (ExtractResult, error) {
	if status == http.StatusOK {
		var reply ExtractResult
		if err := json.Unmarshal(raw, &reply); err != nil {
			return ExtractResult{}, fmt.Errorf("extract returned malformed JSON: %w", err)
		}
		if reply.Network != "tron" || len(reply.Claims) == 0 || len(reply.Claims) > 10 {
			return ExtractResult{}, errors.New("extract reply is not a bounded tron claim set")
		}
		return reply, nil
	}
	var failure extractErrorWire
	if json.Unmarshal(raw, &failure) != nil || failure.Code == "" {
		return ExtractResult{}, fmt.Errorf("extract returned %d", status)
	}
	return ExtractResult{}, &ExtractError{Code: failure.Code, Message: failure.Message, Status: status}
}
