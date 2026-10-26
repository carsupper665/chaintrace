package controller

import (
	"context"
	"regexp"

	"chaintrace/analysis"
)

const (
	maximumClaimUploadBytes = 5 << 20
	// multipartOverhead covers boundary lines and the image field headers so
	// MaxBytesReader can reject the whole request before Gin buffers it.
	multipartOverhead = 64 << 10
	pngMagic          = "\x89PNG\r\n\x1a\n"
	maximumClaimCount = 10
)

var (
	unsignedIntegerPattern = regexp.MustCompile(`^[0-9]+$`)
	transactionHashPattern = regexp.MustCompile(`(?i)^[0-9a-f]{64}$`)
)

// ExtractedClaim is one unverified structured assertion from a screenshot.
type ExtractedClaim struct {
	Kind       string  `json:"kind"`
	Value      string  `json:"value"`
	Role       string  `json:"role,omitempty"`
	Asset      string  `json:"asset,omitempty"`
	Confidence float64 `json:"confidence"`
}

// ClaimExtractResult is the neutral extract outcome at the ClaimExtractor seam.
type ClaimExtractResult struct {
	Network string
	Claims  []ExtractedClaim
}

// ClaimExtractError is a machine-readable failure from the extract seam.
type ClaimExtractError struct {
	Code    string
	Message string
	Status  int
}

func (e *ClaimExtractError) Error() string {
	return e.Code + ": " + e.Message
}

// ClaimExtractor turns sanitized PNG bytes into pending Claims.
type ClaimExtractor interface {
	ExtractPNG(ctx context.Context, image []byte) (ClaimExtractResult, error)
}

// ClaimHandler owns authenticated extract and verification routes.
type ClaimHandler struct {
	extractor ClaimExtractor
	provider  analysis.ChainDataProvider
}

func NewClaimHandler(extractor ClaimExtractor, provider analysis.ChainDataProvider) *ClaimHandler {
	return &ClaimHandler{extractor: extractor, provider: provider}
}
