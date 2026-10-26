package controller

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"chaintrace/analysis"

	"github.com/gin-gonic/gin"
)

// Extract accepts one PNG/JPEG/WebP upload, sanitizes it to PNG, calls the Agent
// extract seam, and returns pending Claims. The image exists only for this request.
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

	raw, err := io.ReadAll(io.LimitReader(opened, maximumClaimUploadBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "invalid_image", "message": "unable to read image",
		})
		return
	}
	if int64(len(raw)) > maximumClaimUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "image_too_large", "message": "image exceeds size limit",
		})
		return
	}
	sanitized, err := sanitizeClaimImage(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": claimImageErrorCode(err), "message": err.Error(),
		})
		return
	}

	result, err := h.extractor.ExtractPNG(c.Request.Context(), sanitized)
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
	claims, err := normalizeExtractedClaims(result)
	if err != nil {
		var extractErr *ClaimExtractError
		if errors.As(err, &extractErr) {
			c.JSON(http.StatusBadRequest, gin.H{"code": extractErr.Code, "message": extractErr.Message})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{
			"code": "malformed_claims", "message": "extract returned an unexpected shape",
		})
		return
	}
	wire := make([]gin.H, 0, len(claims))
	for _, claim := range claims {
		item := gin.H{
			"kind":       claim.Kind,
			"value":      claim.Value,
			"confidence": claim.Confidence,
			"status":     "unverified",
		}
		if claim.Role != "" {
			item["role"] = claim.Role
		}
		if claim.Asset != "" {
			item["asset"] = claim.Asset
		}
		wire = append(wire, item)
	}
	c.JSON(http.StatusOK, gin.H{"network": "tron", "claims": wire})
}

func normalizeExtractedClaims(result ClaimExtractResult) ([]ExtractedClaim, error) {
	if result.Network != "tron" {
		return nil, &ClaimExtractError{Code: "malformed_claims", Message: "network must be tron"}
	}
	if len(result.Claims) == 0 {
		return nil, &ClaimExtractError{Code: "no_claim", Message: "screenshot contained no extractable claim"}
	}
	if len(result.Claims) > maximumClaimCount {
		return nil, &ClaimExtractError{Code: "malformed_claims", Message: "too many claims"}
	}
	normalized := make([]ExtractedClaim, 0, len(result.Claims))
	for _, claim := range result.Claims {
		item, err := validateExtractedClaim(claim)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, item)
	}
	return normalized, nil
}

func validateExtractedClaim(claim ExtractedClaim) (ExtractedClaim, error) {
	if claim.Confidence < 0 || claim.Confidence > 1 {
		return ExtractedClaim{}, &ClaimExtractError{Code: "malformed_claims", Message: "confidence out of range"}
	}
	switch claim.Kind {
	case "address":
		if !IsValidTRONAddress(claim.Value) {
			return ExtractedClaim{}, &ClaimExtractError{Code: "invalid_claim_address", Message: "address failed Base58Check"}
		}
		if claim.Role != "from" && claim.Role != "to" && claim.Role != "unknown" {
			return ExtractedClaim{}, &ClaimExtractError{Code: "malformed_claims", Message: "invalid address role"}
		}
		return ExtractedClaim{Kind: "address", Value: claim.Value, Role: claim.Role, Confidence: claim.Confidence}, nil
	case "tx_hash":
		if !transactionHashPattern.MatchString(claim.Value) {
			return ExtractedClaim{}, &ClaimExtractError{Code: "malformed_claims", Message: "invalid transaction hash"}
		}
		return ExtractedClaim{Kind: "tx_hash", Value: strings.ToLower(claim.Value), Confidence: claim.Confidence}, nil
	case "amount":
		if claim.Asset != analysis.AssetUSDT || !unsignedIntegerPattern.MatchString(claim.Value) {
			return ExtractedClaim{}, &ClaimExtractError{Code: "malformed_claims", Message: "invalid amount claim"}
		}
		return ExtractedClaim{Kind: "amount", Value: claim.Value, Asset: analysis.AssetUSDT, Confidence: claim.Confidence}, nil
	default:
		return ExtractedClaim{}, &ClaimExtractError{Code: "malformed_claims", Message: "unknown claim kind"}
	}
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
