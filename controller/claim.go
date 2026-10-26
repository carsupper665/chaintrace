package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
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

type verifyAddressRequest struct {
	Address string `json:"address"`
}

// VerifyAddress checks that a TRON address has on-chain USDT activity.
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
	status, err := h.probeAddressActivity(c.Request.Context(), request.Address)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"code": "provider_unavailable", "message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"address": request.Address,
		"network": "tron",
		"status":  status,
	})
}

type verifyClaimsRequest struct {
	SelectedAddress string           `json:"selectedAddress"`
	Claims          []ExtractedClaim `json:"claims"`
}

// VerifyClaims verifies a bounded Claim set. Address-only checks use activity
// probing; a tx_hash set matches a single confirmed USDT Transfer event.
func (h *ClaimHandler) VerifyClaims(c *gin.Context) {
	var request verifyClaimsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "validation_error", "message": "Invalid request",
		})
		return
	}
	claims, err := normalizeExtractedClaims(ClaimExtractResult{Network: "tron", Claims: request.Claims})
	if err != nil {
		var extractErr *ClaimExtractError
		if errors.As(err, &extractErr) {
			c.JSON(http.StatusBadRequest, gin.H{"code": extractErr.Code, "message": extractErr.Message})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "malformed_claims", "message": "invalid claims"})
		return
	}
	if request.SelectedAddress != "" && !IsValidTRONAddress(request.SelectedAddress) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "invalid_tron_target", "message": "Invalid TRON target",
		})
		return
	}

	hasTx := false
	for _, claim := range claims {
		if claim.Kind == "tx_hash" {
			hasTx = true
			break
		}
	}
	if hasTx {
		response, err := h.verifyTransferClaims(c.Request.Context(), request.SelectedAddress, claims)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{
				"code": "provider_unavailable", "message": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, response)
		return
	}

	if request.SelectedAddress == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "validation_error", "message": "selectedAddress is required",
		})
		return
	}
	status, err := h.probeAddressActivity(c.Request.Context(), request.SelectedAddress)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"code": "provider_unavailable", "message": err.Error(),
		})
		return
	}
	results := make([]gin.H, 0, len(claims))
	for _, claim := range claims {
		item := gin.H{"kind": claim.Kind, "value": claim.Value, "status": "unverified"}
		if claim.Kind == "address" && claim.Value == request.SelectedAddress {
			item["status"] = status
		}
		results = append(results, item)
	}
	payload := gin.H{"results": results}
	if status == "verified" {
		payload["prefillAddress"] = request.SelectedAddress
	}
	c.JSON(http.StatusOK, payload)
}

func (h *ClaimHandler) probeAddressActivity(ctx context.Context, address string) (string, error) {
	if h.provider == nil {
		return "", errors.New("chain data provider is not available")
	}
	cutoff, err := h.provider.CaptureConfirmedCutoff(ctx, "TRON_MAINNET")
	if err != nil {
		return "", errors.New("unable to capture confirmed cutoff")
	}
	page, err := h.provider.FetchAddressTransfers(ctx, analysis.AddressTransferPageRequest{
		Address:        address,
		Network:        "TRON_MAINNET",
		Asset:          analysis.AssetUSDT,
		WindowStart:    cutoff.Timestamp.Add(-30 * 24 * time.Hour),
		WindowEnd:      cutoff.Timestamp,
		CutoffBlockID:  cutoff.BlockID,
		TransferLimit:  1,
		TraversalDepth: 1,
	})
	if err != nil {
		return "", errors.New("unable to probe address activity")
	}
	if addressHasTransferActivity(page) {
		return "verified", nil
	}
	return "no_activity", nil
}

func (h *ClaimHandler) verifyTransferClaims(
	ctx context.Context, selectedAddress string, claims []ExtractedClaim,
) (gin.H, error) {
	if h.provider == nil {
		return nil, errors.New("chain data provider is not available")
	}
	var (
		hashClaim   *ExtractedClaim
		amountClaim *ExtractedClaim
		addresses   []ExtractedClaim
	)
	for index := range claims {
		claim := claims[index]
		switch claim.Kind {
		case "tx_hash":
			if hashClaim != nil {
				return nil, errors.New("multiple transaction hashes are not supported")
			}
			hashClaim = &claims[index]
		case "amount":
			amountClaim = &claims[index]
		case "address":
			addresses = append(addresses, claim)
		}
	}
	if hashClaim == nil {
		return nil, errors.New("transaction hash is required")
	}
	tx, found, err := h.provider.LookupConfirmedUSDTTransaction(ctx, hashClaim.Value)
	if err != nil {
		return nil, errors.New("unable to look up transaction")
	}
	results := make([]gin.H, 0, len(claims))
	if !found {
		for _, claim := range claims {
			status := "unverified"
			if claim.Kind == "tx_hash" {
				status = "not_found"
			}
			results = append(results, gin.H{"kind": claim.Kind, "value": claim.Value, "status": status})
		}
		return gin.H{"results": results}, nil
	}
	if !tx.Successful || !tx.Confirmed {
		for _, claim := range claims {
			status := "unverified"
			if claim.Kind == "tx_hash" {
				status = "mismatch"
			}
			item := gin.H{"kind": claim.Kind, "value": claim.Value, "status": status}
			if claim.Kind == "tx_hash" {
				item["actual"] = gin.H{"successful": tx.Successful, "confirmed": tx.Confirmed}
			}
			results = append(results, item)
		}
		return gin.H{"results": results}, nil
	}

	matched := matchTransferClaims(tx, selectedAddress, amountClaim, addresses)
	for _, claim := range claims {
		item := gin.H{"kind": claim.Kind, "value": claim.Value, "status": "unverified"}
		switch claim.Kind {
		case "tx_hash":
			if matched.ok {
				item["status"] = "verified"
			} else {
				item["status"] = "mismatch"
				item["actual"] = matched.actual
			}
		case "amount":
			if matched.ok && amountClaim != nil && claim.Value == amountClaim.Value {
				item["status"] = "verified"
			} else if amountClaim != nil {
				item["status"] = "mismatch"
				if matched.transfer != nil {
					item["actual"] = gin.H{
						"amount":   matched.transfer.AmountSmallestUnit,
						"decimals": matched.transfer.Decimals,
						"asset":    matched.transfer.Asset,
					}
				}
			}
		case "address":
			if matched.ok && matched.verifiedAddresses[claim.Value] {
				item["status"] = "verified"
			} else if selectedAddress != "" && claim.Value == selectedAddress {
				item["status"] = "mismatch"
			}
		}
		results = append(results, item)
	}
	payload := gin.H{"results": results}
	if matched.ok && selectedAddress != "" && matched.verifiedAddresses[selectedAddress] {
		payload["prefillAddress"] = selectedAddress
	}
	return payload, nil
}

type transferMatch struct {
	ok                bool
	transfer          *analysis.TRC20Transfer
	verifiedAddresses map[string]bool
	actual            gin.H
}

func matchTransferClaims(
	tx analysis.BlockchainTransaction,
	selectedAddress string,
	amountClaim *ExtractedClaim,
	addresses []ExtractedClaim,
) transferMatch {
	result := transferMatch{verifiedAddresses: map[string]bool{}}
	actualTransfers := make([]gin.H, 0, len(tx.Transfers))
	for _, transfer := range tx.Transfers {
		actualTransfers = append(actualTransfers, gin.H{
			"from":     transfer.FromAddress,
			"to":       transfer.ToAddress,
			"amount":   transfer.AmountSmallestUnit,
			"decimals": transfer.Decimals,
			"asset":    transfer.Asset,
			"event":    transfer.EventIdentity,
		})
	}
	result.actual = gin.H{"transfers": actualTransfers}

	for index := range tx.Transfers {
		transfer := &tx.Transfers[index]
		if transfer.ContractAddress != analysis.TRONMainnetUSDTContract || transfer.Asset != analysis.AssetUSDT {
			continue
		}
		if amountClaim != nil && transfer.AmountSmallestUnit != amountClaim.Value {
			continue
		}
		roleOK := true
		verified := map[string]bool{}
		for _, addressClaim := range addresses {
			switch addressClaim.Role {
			case "from":
				if addressClaim.Value != transfer.FromAddress {
					roleOK = false
				} else {
					verified[addressClaim.Value] = true
				}
			case "to":
				if addressClaim.Value != transfer.ToAddress {
					roleOK = false
				} else {
					verified[addressClaim.Value] = true
				}
			default:
				if addressClaim.Value == transfer.FromAddress || addressClaim.Value == transfer.ToAddress {
					verified[addressClaim.Value] = true
				} else {
					roleOK = false
				}
			}
			if !roleOK {
				break
			}
		}
		if !roleOK {
			continue
		}
		if selectedAddress != "" && !verified[selectedAddress] &&
			selectedAddress != transfer.FromAddress && selectedAddress != transfer.ToAddress {
			continue
		}
		if selectedAddress != "" {
			verified[selectedAddress] = true
		}
		result.ok = true
		result.transfer = transfer
		result.verifiedAddresses = verified
		return result
	}
	return result
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
