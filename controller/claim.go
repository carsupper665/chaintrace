package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"chaintrace/analysis"

	"github.com/gin-gonic/gin"
)

const (
	maximumClaimUploadBytes = 5 << 20
	// multipartOverhead covers boundary lines and the image field headers so
	// MaxBytesReader can reject the whole request before Gin buffers it.
	multipartOverhead = 64 << 10
	pngMagic          = "\x89PNG\r\n\x1a\n"
)

// AddressClaim is one unverified address assertion extracted from a screenshot.
type AddressClaim struct {
	Kind       string
	Value      string
	Role       string
	Confidence float64
}

// ClaimExtractResult is the neutral extract outcome at the ClaimExtractor seam.
type ClaimExtractResult struct {
	Network string
	Claims  []AddressClaim
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

// ClaimExtractor turns screenshot bytes into a pending address Claim.
// The production implementation adapts claimclient.Client; HTTP tests inject a fake.
type ClaimExtractor interface {
	ExtractPNG(ctx context.Context, image []byte) (ClaimExtractResult, error)
}

// ClaimHandler owns the authenticated extract and address-activity routes.
type ClaimHandler struct {
	extractor ClaimExtractor
	provider  analysis.ChainDataProvider
}

func NewClaimHandler(extractor ClaimExtractor, provider analysis.ChainDataProvider) *ClaimHandler {
	return &ClaimHandler{extractor: extractor, provider: provider}
}

// Extract accepts one PNG upload, calls the Agent extract seam, and returns
// pending Claims. The image exists only for this request.
func (h *ClaimHandler) Extract(c *gin.Context) {
	if h.extractor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "agent_unavailable", "message": "Claim extraction is not available",
		})
		return
	}
	c.Request.Body = http.MaxBytesReader(
		c.Writer, c.Request.Body, maximumClaimUploadBytes+multipartOverhead,
	)
	file, err := c.FormFile("image")
	if err != nil {
		if isRequestBodyTooLarge(err) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": "image_too_large", "message": "image exceeds size limit",
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "validation_error", "message": "image file is required",
		})
		return
	}
	if file.Size > maximumClaimUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "image_too_large", "message": "image exceeds size limit",
		})
		return
	}
	opened, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "invalid_image", "message": "unable to read image",
		})
		return
	}
	defer opened.Close()

	image, err := io.ReadAll(io.LimitReader(opened, maximumClaimUploadBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "invalid_image", "message": "unable to read image",
		})
		return
	}
	if int64(len(image)) > maximumClaimUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "image_too_large", "message": "image exceeds size limit",
		})
		return
	}
	if len(image) < len(pngMagic) || string(image[:len(pngMagic)]) != pngMagic {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "invalid_image", "message": "only PNG is accepted",
		})
		return
	}

	result, err := h.extractor.ExtractPNG(c.Request.Context(), image)
	if err != nil {
		var extractErr *ClaimExtractError
		if errors.As(err, &extractErr) {
			status := http.StatusBadRequest
			if extractErr.Status >= 500 {
				status = http.StatusBadGateway
			}
			c.JSON(status, gin.H{"code": extractErr.Code, "message": extractErr.Message})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{
			"code": "agent_unavailable", "message": "Claim extraction failed",
		})
		return
	}
	if result.Network != "tron" || len(result.Claims) != 1 {
		c.JSON(http.StatusBadGateway, gin.H{
			"code": "malformed_claims", "message": "extract returned an unexpected shape",
		})
		return
	}
	claim := result.Claims[0]
	if claim.Kind != "address" || !IsValidTRONAddress(claim.Value) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "invalid_claim_address", "message": "extracted address failed Base58Check",
		})
		return
	}
	if claim.Role != "from" && claim.Role != "to" && claim.Role != "unknown" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "malformed_claims", "message": "extracted claim role is invalid",
		})
		return
	}
	if claim.Confidence < 0 || claim.Confidence > 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "malformed_claims", "message": "extracted claim confidence is out of range",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"network": "tron",
		"claims": []gin.H{{
			"kind":       "address",
			"value":      claim.Value,
			"role":       claim.Role,
			"confidence": claim.Confidence,
			"status":     "unverified",
		}},
	})
}

type verifyAddressRequest struct {
	Address string `json:"address"`
}

// VerifyAddress checks that a TRON address has on-chain USDT activity via the
// existing ChainDataProvider seam. Only "verified" addresses may prefill a Target.
func (h *ClaimHandler) VerifyAddress(c *gin.Context) {
	var request verifyAddressRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Address == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "validation_error", "message": "Invalid request",
		})
		return
	}
	if !IsValidTRONAddress(request.Address) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "invalid_tron_target", "message": "Invalid TRON target",
		})
		return
	}
	if h.provider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "provider_unavailable", "message": "Chain data provider is not available",
		})
		return
	}

	cutoff, err := h.provider.CaptureConfirmedCutoff(c.Request.Context(), "TRON_MAINNET")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"code": "provider_unavailable", "message": "Unable to capture confirmed cutoff",
		})
		return
	}
	page, err := h.provider.FetchAddressTransfers(c.Request.Context(), analysis.AddressTransferPageRequest{
		Address:        request.Address,
		Network:        "TRON_MAINNET",
		Asset:          analysis.AssetUSDT,
		WindowStart:    cutoff.Timestamp.Add(-30 * 24 * time.Hour),
		WindowEnd:      cutoff.Timestamp,
		CutoffBlockID:  cutoff.BlockID,
		TransferLimit:  1,
		TraversalDepth: 1,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"code": "provider_unavailable", "message": "Unable to probe address activity",
		})
		return
	}
	status := "no_activity"
	if addressHasTransferActivity(page) {
		status = "verified"
	}
	c.JSON(http.StatusOK, gin.H{
		"address": request.Address,
		"network": "tron",
		"status":  status,
	})
}

func addressHasTransferActivity(page analysis.AddressTransferPage) bool {
	for _, transaction := range page.Transactions {
		if !transaction.Successful || !transaction.Confirmed {
			continue
		}
		if len(transaction.Transfers) > 0 {
			return true
		}
	}
	return false
}

func isRequestBodyTooLarge(err error) bool {
	if err == nil {
		return false
	}
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "request body too large")
}
