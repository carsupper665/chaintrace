package controller

import (
	"context"
	"errors"
	"net/http"
	"time"

	"chaintrace/analysis"

	"github.com/gin-gonic/gin"
)

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
	if request.SelectedAddress == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "validation_error", "message": "selectedAddress is required",
		})
		return
	}
	if !IsValidTRONAddress(request.SelectedAddress) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "invalid_tron_target", "message": "Invalid TRON target",
		})
		return
	}
	if !addressClaimContains(claims, request.SelectedAddress) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "validation_error", "message": "selectedAddress must be an address claim",
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

func addressClaimContains(claims []ExtractedClaim, address string) bool {
	for _, claim := range claims {
		if claim.Kind == "address" && claim.Value == address {
			return true
		}
	}
	return false
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
		actual := gin.H{"successful": tx.Successful, "confirmed": tx.Confirmed}
		for _, claim := range claims {
			status := "unverified"
			item := gin.H{"kind": claim.Kind, "value": claim.Value, "status": status}
			if claim.Kind == "tx_hash" {
				item["status"] = "mismatch"
				item["actual"] = actual
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
				item["actual"] = amountActual(matched)
			}
		case "address":
			if matched.ok && matched.verifiedAddresses[claim.Value] {
				item["status"] = "verified"
			} else {
				item["status"] = "mismatch"
				item["actual"] = addressActual(matched, claim.Value)
			}
		}
		results = append(results, item)
	}
	payload := gin.H{"results": results}
	if matched.ok && matched.verifiedAddresses[selectedAddress] {
		payload["prefillAddress"] = selectedAddress
	}
	return payload, nil
}

func amountActual(matched transferMatch) gin.H {
	if matched.transfer != nil {
		return gin.H{
			"amount":   matched.transfer.AmountSmallestUnit,
			"decimals": matched.transfer.Decimals,
			"asset":    matched.transfer.Asset,
			"from":     matched.transfer.FromAddress,
			"to":       matched.transfer.ToAddress,
		}
	}
	return matched.actual
}

func addressActual(matched transferMatch, claimed string) gin.H {
	actual := gin.H{"claimed": claimed}
	if matched.transfer != nil {
		actual["from"] = matched.transfer.FromAddress
		actual["to"] = matched.transfer.ToAddress
		actual["amount"] = matched.transfer.AmountSmallestUnit
		return actual
	}
	if transfers, ok := matched.actual["transfers"]; ok {
		actual["transfers"] = transfers
	}
	return actual
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
		// selectedAddress must already be proven by an address Claim on this event.
		if selectedAddress != "" && !verified[selectedAddress] {
			continue
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
