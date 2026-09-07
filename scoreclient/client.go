package scoreclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"chaintrace/analysis"
)

// maximumScoreResponse bounds what the scoring route can make this process
// allocate. The reply is one float and a hash string, unlike agentclient's
// chat reply — a small cap is generous.
const maximumScoreResponse = 16 << 10

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
	Decimals      int            `json:"decimals"`
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

// New returns nil when no scorer is configured, the same "untyped nil means
// disabled" convention as agentclient.New — callers must assign the result to
// a analysis.LearnedScorer variable carefully so a nil *Client does not
// become a non-nil interface wrapping a nil pointer.
func New(options Options) *Client {
	if !options.Enabled() {
		return nil
	}
	options = options.withDefaults()
	return &Client{
		baseURL:   strings.TrimRight(options.BaseURL, "/"),
		sharedKey: options.SharedKey,
		client:    &http.Client{Timeout: options.HTTPTimeout},
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
		Decimals:      input.Decimals,
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
		return analysis.LearnedScoreResult{Score: reply.Score, Source: reply.ModelVersion}, nil
	}
	var failure scoreErrorWire
	if json.Unmarshal(raw, &failure) != nil || failure.Code == "" {
		return analysis.LearnedScoreResult{}, fmt.Errorf("scorer returned %d", status)
	}
	return analysis.LearnedScoreResult{}, fmt.Errorf("scorer %s: %s", failure.Code, failure.Message)
}
