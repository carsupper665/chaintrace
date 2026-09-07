// Package scoreclient calls the Python Agent sidecar's POST /v1/score route —
// the learned risk scorer (docs/adr/0015-learned-risk-scoring-alongside-rules.md).
//
// It is a separate, small package from agentclient rather than folded into
// it: agentclient speaks the chat/tool-loop protocol to the same sidecar
// process, an unrelated and much larger concern. This package's only job is
// one request, one response.
package scoreclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"chaintrace/analysis"
)

const (
	// maximumScoreResponse bounds what the scoring route can make this process
	// allocate. The reply is one float and a hash string, unlike agentclient's
	// chat reply — a small cap is generous.
	maximumScoreResponse = 16 << 10
	// scoreTimeout bounds one call to the sidecar, the 30 seconds
	// docs/development-rules.md section 8 sets for every Go call into Python.
	// The caller's context bounds the whole attempt.
	scoreTimeout = 30 * time.Second
)

type transferWire struct {
	FromAddress string `json:"from_address"`
	ToAddress   string `json:"to_address"`
	// Amount stays the smallest-unit string unconverted — this is the exact
	// precision above 2^53 guarantee agent/scoring/features.py depends on.
	Amount      string `json:"amount"`
	TimestampMs int64  `json:"timestamp_ms"`
}

type scoreRequestWire struct {
	TargetAddress string         `json:"target_address"`
	WindowStartMs int64          `json:"window_start_ms"`
	WindowEndMs   int64          `json:"window_end_ms"`
	Truncated     bool           `json:"truncated"`
	Transfers     []transferWire `json:"transfers"`
}

type scoreResponseWire struct {
	Score        float64 `json:"score"`
	ModelVersion string  `json:"model_version"`
}

type scoreErrorWire struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Client implements analysis.LearnedScorer over HTTP.
type Client struct {
	baseURL   string
	sharedKey string
	client    *http.Client
}

// New returns nil when the sidecar is not configured, the same "untyped nil
// means disabled" convention as agentclient.New — callers must assign the
// result to an analysis.LearnedScorer variable carefully so a nil *Client
// does not become a non-nil interface wrapping a nil pointer. Both values are
// required: an unauthenticated sidecar would accept requests from anything on
// the LAN.
func New(baseURL, sharedKey string) *Client {
	if baseURL == "" || sharedKey == "" {
		return nil
	}
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		sharedKey: sharedKey,
		client:    &http.Client{Timeout: scoreTimeout},
	}
}

func (c *Client) Score(ctx context.Context, input analysis.LearnedScoreInput) (analysis.LearnedScoreResult, error) {
	transfers := make([]transferWire, len(input.Transfers))
	for i, transfer := range input.Transfers {
		transfers[i] = transferWire{
			FromAddress: transfer.FromAddress,
			ToAddress:   transfer.ToAddress,
			Amount:      transfer.AmountSmallestUnit,
			TimestampMs: transfer.Timestamp.UnixMilli(),
		}
	}
	body, err := json.Marshal(scoreRequestWire{
		TargetAddress: input.TargetAddress,
		WindowStartMs: input.WindowStart.UnixMilli(),
		WindowEndMs:   input.WindowEnd.UnixMilli(),
		Truncated:     input.Truncated,
		Transfers:     transfers,
	})
	if err != nil {
		return analysis.LearnedScoreResult{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/score", bytes.NewReader(body))
	if err != nil {
		return analysis.LearnedScoreResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Agent-Key", c.sharedKey)

	response, err := c.client.Do(request)
	if err != nil {
		return analysis.LearnedScoreResult{}, err
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumScoreResponse))
	if err != nil {
		return analysis.LearnedScoreResult{}, err
	}
	return decodeScoreResponse(response.StatusCode, raw)
}

func decodeScoreResponse(status int, raw []byte) (analysis.LearnedScoreResult, error) {
	if status == http.StatusOK {
		var reply scoreResponseWire
		if err := json.Unmarshal(raw, &reply); err != nil {
			return analysis.LearnedScoreResult{}, fmt.Errorf("scorer returned malformed JSON: %w", err)
		}
		// An empty model_version means this was not a scoring reply at all —
		// `{}` decodes happily into a zero score, which would otherwise be
		// stored as a real measurement.
		if reply.ModelVersion == "" {
			return analysis.LearnedScoreResult{}, errors.New("scorer reply carries no model version")
		}
		return analysis.LearnedScoreResult{Score: reply.Score, Source: reply.ModelVersion}, nil
	}
	var failure scoreErrorWire
	if json.Unmarshal(raw, &failure) != nil || failure.Code == "" {
		return analysis.LearnedScoreResult{}, fmt.Errorf("scorer returned %d", status)
	}
	return analysis.LearnedScoreResult{}, fmt.Errorf("scorer %s: %s", failure.Code, failure.Message)
}
