package router_test

import (
	"bytes"
	"chaintrace/analysis"
	"chaintrace/controller"
	apiRouter "chaintrace/router"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

var claimTestPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

type fixtureClaimExtractor struct {
	result controller.ClaimExtractResult
	err    error
	calls  int
	last   []byte
}

func (f *fixtureClaimExtractor) ExtractPNG(_ context.Context, image []byte) (controller.ClaimExtractResult, error) {
	f.calls++
	f.last = append([]byte(nil), image...)
	return f.result, f.err
}

func TestUnauthenticatedClaimExtractIsRejectedBeforeAgent(t *testing.T) {
	test := newAuthHTTPTest(t)
	extractor := &fixtureClaimExtractor{
		result: controller.ClaimExtractResult{
			Network: "tron",
			Claims: []controller.ExtractedClaim{{
				Kind: "address", Value: testTargetAddress, Role: "to", Confidence: 0.9,
			}},
		},
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithClaimOptions(test.engine, analysis.Options{
		Provider: successfulProvider(time.Now().UTC()),
	}, extractor)

	response := multipartClaimExtract(t, test, "", claimTestPNG)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	if extractor.calls != 0 {
		t.Fatalf("extractor calls = %d, want 0 before auth", extractor.calls)
	}
}

func TestOwnerCanExtractPendingAddressClaimFromPNG(t *testing.T) {
	test := newAuthHTTPTest(t)
	extractor := &fixtureClaimExtractor{
		result: controller.ClaimExtractResult{
			Network: "tron",
			Claims: []controller.ExtractedClaim{{
				Kind: "address", Value: "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj",
				Role: "to", Confidence: 0.92,
			}},
		},
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithClaimOptions(test.engine, analysis.Options{
		Provider: successfulProvider(time.Now().UTC()),
	}, extractor)
	token := ownerToken(t, test.owner.ID)

	response := multipartClaimExtract(t, test, token, claimTestPNG)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	if extractor.calls != 1 || len(extractor.last) < 8 || string(extractor.last[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("extractor should receive sanitized PNG, got calls=%d last=%#v", extractor.calls, extractor.last)
	}
	var body struct {
		Network string `json:"network"`
		Claims  []struct {
			Kind       string  `json:"kind"`
			Value      string  `json:"value"`
			Role       string  `json:"role"`
			Confidence float64 `json:"confidence"`
			Status     string  `json:"status"`
		} `json:"claims"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Network != "tron" || len(body.Claims) != 1 {
		t.Fatalf("body = %#v", body)
	}
	claim := body.Claims[0]
	if claim.Kind != "address" || claim.Value != "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj" ||
		claim.Role != "to" || claim.Confidence != 0.92 || claim.Status != "unverified" {
		t.Fatalf("claim = %#v", claim)
	}
}

func TestExtractRejectsInvalidClaimAddressFromAgent(t *testing.T) {
	test := newAuthHTTPTest(t)
	extractor := &fixtureClaimExtractor{
		result: controller.ClaimExtractResult{
			Network: "tron",
			Claims: []controller.ExtractedClaim{{
				Kind: "address", Value: "not-valid", Role: "to", Confidence: 0.5,
			}},
		},
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithClaimOptions(test.engine, analysis.Options{}, extractor)
	token := ownerToken(t, test.owner.ID)

	response := multipartClaimExtract(t, test, token, claimTestPNG)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	assertErrorCode(t, response, "invalid_claim_address")
}

func TestExtractRejectsOversizedMultipartBeforeAgent(t *testing.T) {
	test := newAuthHTTPTest(t)
	extractor := &fixtureClaimExtractor{}
	test.engine = gin.New()
	apiRouter.ApiRouterWithClaimOptions(test.engine, analysis.Options{}, extractor)
	token := ownerToken(t, test.owner.ID)

	oversized := bytes.Repeat([]byte("a"), (5<<20)+128<<10)
	response := multipartClaimExtract(t, test, token, oversized)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	assertErrorCode(t, response, "image_too_large")
	if extractor.calls != 0 {
		t.Fatalf("extractor calls = %d, want 0 for oversized body", extractor.calls)
	}
}

func TestVerifyAddressReportsActivityFromProvider(t *testing.T) {
	test := newAuthHTTPTest(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	provider := successfulProvider(cutoff)
	test.engine = gin.New()
	apiRouter.ApiRouterWithClaimOptions(test.engine, analysis.Options{Provider: provider}, nil)
	token := ownerToken(t, test.owner.ID)

	verified := test.request(http.MethodPost, "/api/v1/claims/verify-address", map[string]string{
		"address": testTargetAddress,
	}, token)
	if verified.Code != http.StatusOK {
		t.Fatalf("verified status = %d; body: %s", verified.Code, verified.Body.String())
	}
	assertJSONField(t, verified.Body.Bytes(), "status", "verified")

	idle := test.request(http.MethodPost, "/api/v1/claims/verify-address", map[string]string{
		"address": "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj",
	}, token)
	if idle.Code != http.StatusOK {
		t.Fatalf("idle status = %d; body: %s", idle.Code, idle.Body.String())
	}
	assertJSONField(t, idle.Body.Bytes(), "status", "no_activity")
}

func TestClaimExtractThenVerifyThenCreateInvestigation(t *testing.T) {
	test := newAuthHTTPTest(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	extractor := &fixtureClaimExtractor{
		result: controller.ClaimExtractResult{
			Network: "tron",
			Claims: []controller.ExtractedClaim{{
				Kind: "address", Value: testTargetAddress, Role: "to", Confidence: 0.88,
			}},
		},
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithClaimOptions(test.engine, analysis.Options{
		Provider: successfulProvider(cutoff),
	}, extractor)
	token := ownerToken(t, test.owner.ID)

	extracted := multipartClaimExtract(t, test, token, claimTestPNG)
	if extracted.Code != http.StatusOK {
		t.Fatalf("extract status = %d; body: %s", extracted.Code, extracted.Body.String())
	}

	verified := test.request(http.MethodPost, "/api/v1/claims/verify-address", map[string]string{
		"address": testTargetAddress,
	}, token)
	if verified.Code != http.StatusOK {
		t.Fatalf("verify status = %d; body: %s", verified.Code, verified.Body.String())
	}
	assertJSONField(t, verified.Body.Bytes(), "status", "verified")

	// Prefill uses the existing Investigation create + target update path — no
	// second create API. Owner still confirms by writing the Target.
	created := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{
		"title": "From claim",
	}, token)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body: %s", created.Code, created.Body.String())
	}
	var creation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &creation); err != nil || creation.ID == "" {
		t.Fatalf("decode creation: %v body=%s", err, created.Body.String())
	}
	confirmed := test.request(http.MethodPatch, "/api/v1/investigations/"+creation.ID, map[string]string{
		"address": testTargetAddress,
	}, token)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm target status = %d; body: %s", confirmed.Code, confirmed.Body.String())
	}
	assertJSONField(t, confirmed.Body.Bytes(), "address", testTargetAddress)
}

func TestOwnerCanExtractJPEGAndRejectFakeMIME(t *testing.T) {
	test := newAuthHTTPTest(t)
	extractor := &fixtureClaimExtractor{
		result: controller.ClaimExtractResult{
			Network: "tron",
			Claims: []controller.ExtractedClaim{{
				Kind: "address", Value: testTargetAddress, Role: "to", Confidence: 0.9,
			}},
		},
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithClaimOptions(test.engine, analysis.Options{}, extractor)
	token := ownerToken(t, test.owner.ID)

	jpegBytes := encodeClaimTestJPEG(t)
	ok := multipartClaimExtractNamed(t, test, token, jpegBytes, "shot.jpg")
	if ok.Code != http.StatusOK {
		t.Fatalf("jpeg extract status = %d; body: %s", ok.Code, ok.Body.String())
	}
	if extractor.calls != 1 || len(extractor.last) < 8 || string(extractor.last[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("extractor should receive re-encoded PNG, got calls=%d len=%d", extractor.calls, len(extractor.last))
	}

	extractor.calls = 0
	fake := multipartClaimExtractNamed(t, test, token, []byte("GIF89a-not-image"), "shot.png")
	if fake.Code != http.StatusBadRequest {
		t.Fatalf("fake mime status = %d; body: %s", fake.Code, fake.Body.String())
	}
	assertErrorCode(t, fake, "unsupported_media")
	if extractor.calls != 0 {
		t.Fatalf("extractor calls = %d, want 0 for unsupported media", extractor.calls)
	}
}

func TestVerifyClaimsMatchesConfirmedUSDTTransfer(t *testing.T) {
	test := newAuthHTTPTest(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	provider := successfulProvider(cutoff)
	test.engine = gin.New()
	apiRouter.ApiRouterWithClaimOptions(test.engine, analysis.Options{Provider: provider}, nil)
	token := ownerToken(t, test.owner.ID)
	txHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	verified := test.request(http.MethodPost, "/api/v1/claims/verify", map[string]any{
		"selectedAddress": testPeerAddress,
		"claims": []map[string]any{
			{"kind": "tx_hash", "value": txHash, "confidence": 0.9},
			{"kind": "address", "value": testTargetAddress, "role": "from", "confidence": 0.9},
			{"kind": "address", "value": testPeerAddress, "role": "to", "confidence": 0.9},
			{"kind": "amount", "value": testLargeAmount, "asset": "USDT", "confidence": 0.9},
		},
	}, token)
	if verified.Code != http.StatusOK {
		t.Fatalf("verify status = %d; body: %s", verified.Code, verified.Body.String())
	}
	var body struct {
		PrefillAddress string `json:"prefillAddress"`
		Results        []struct {
			Kind   string `json:"kind"`
			Value  string `json:"value"`
			Status string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(verified.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.PrefillAddress != testPeerAddress {
		t.Fatalf("prefill = %q", body.PrefillAddress)
	}
	for _, result := range body.Results {
		if result.Status != "verified" {
			t.Fatalf("result = %#v", result)
		}
	}

	missing := test.request(http.MethodPost, "/api/v1/claims/verify", map[string]any{
		"selectedAddress": testPeerAddress,
		"claims": []map[string]any{
			{"kind": "tx_hash", "value": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "confidence": 0.9},
			{"kind": "address", "value": testPeerAddress, "role": "to", "confidence": 0.9},
		},
	}, token)
	if missing.Code != http.StatusOK {
		t.Fatalf("missing status = %d; body: %s", missing.Code, missing.Body.String())
	}
	var missingBody struct {
		PrefillAddress string `json:"prefillAddress"`
		Results        []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(missing.Body.Bytes(), &missingBody); err != nil {
		t.Fatalf("decode missing: %v", err)
	}
	if missingBody.PrefillAddress != "" || missingBody.Results[0].Status != "not_found" {
		t.Fatalf("missing body = %#v", missingBody)
	}

	mismatch := test.request(http.MethodPost, "/api/v1/claims/verify", map[string]any{
		"selectedAddress": testPeerAddress,
		"claims": []map[string]any{
			{"kind": "tx_hash", "value": txHash, "confidence": 0.9},
			{"kind": "address", "value": testPeerAddress, "role": "to", "confidence": 0.9},
			{"kind": "amount", "value": "1", "asset": "USDT", "confidence": 0.9},
		},
	}, token)
	if mismatch.Code != http.StatusOK {
		t.Fatalf("mismatch status = %d; body: %s", mismatch.Code, mismatch.Body.String())
	}
	var mismatchBody struct {
		PrefillAddress string `json:"prefillAddress"`
		Results        []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(mismatch.Body.Bytes(), &mismatchBody); err != nil {
		t.Fatalf("decode mismatch: %v", err)
	}
	if mismatchBody.PrefillAddress != "" {
		t.Fatalf("mismatch should not prefill: %#v", mismatchBody)
	}
	foundAmountMismatch := false
	for _, result := range mismatchBody.Results {
		if result.Kind == "amount" && result.Status == "mismatch" {
			foundAmountMismatch = true
		}
	}
	if !foundAmountMismatch {
		t.Fatalf("expected amount mismatch, got %#v", mismatchBody.Results)
	}
}

func TestVerifyClaimsRejectsFailedPendingAndWrongContract(t *testing.T) {
	test := newAuthHTTPTest(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	txHash := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	provider := &fixtureChainDataProvider{
		cutoff: analysis.BlockCutoff{BlockID: "0000000000012345", Timestamp: cutoff},
		collection: analysis.Collection{
			Transactions: []analysis.BlockchainTransaction{
				{
					Network: "TRON_MAINNET", Hash: txHash, Successful: false, Confirmed: false,
				},
			},
		},
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithClaimOptions(test.engine, analysis.Options{Provider: provider}, nil)
	token := ownerToken(t, test.owner.ID)

	failed := test.request(http.MethodPost, "/api/v1/claims/verify", map[string]any{
		"selectedAddress": testPeerAddress,
		"claims": []map[string]any{
			{"kind": "tx_hash", "value": txHash, "confidence": 0.9},
			{"kind": "address", "value": testPeerAddress, "role": "to", "confidence": 0.9},
		},
	}, token)
	if failed.Code != http.StatusOK {
		t.Fatalf("failed status = %d; body: %s", failed.Code, failed.Body.String())
	}
	var failedBody struct {
		PrefillAddress string `json:"prefillAddress"`
		Results        []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(failed.Body.Bytes(), &failedBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if failedBody.PrefillAddress != "" || failedBody.Results[0].Status != "mismatch" {
		t.Fatalf("failed body = %#v", failedBody)
	}

	wrongContractHash := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	provider.collection = analysis.Collection{
		Transactions: []analysis.BlockchainTransaction{{
			Network: "TRON_MAINNET", Hash: wrongContractHash, Successful: true, Confirmed: true,
			Transfers: []analysis.TRC20Transfer{{
				EventIdentity: "log:0", ContractAddress: "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf",
				Asset: "USDC", FromAddress: testTargetAddress, ToAddress: testPeerAddress,
				AmountSmallestUnit: "100", Decimals: 6, Timestamp: cutoff,
			}},
		}},
	}
	wrong := test.request(http.MethodPost, "/api/v1/claims/verify", map[string]any{
		"selectedAddress": testPeerAddress,
		"claims": []map[string]any{
			{"kind": "tx_hash", "value": wrongContractHash, "confidence": 0.9},
			{"kind": "address", "value": testPeerAddress, "role": "to", "confidence": 0.9},
			{"kind": "amount", "value": "100", "asset": "USDT", "confidence": 0.9},
		},
	}, token)
	if wrong.Code != http.StatusOK {
		t.Fatalf("wrong status = %d; body: %s", wrong.Code, wrong.Body.String())
	}
	var wrongBody struct {
		PrefillAddress string `json:"prefillAddress"`
		Results        []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(wrong.Body.Bytes(), &wrongBody); err != nil {
		t.Fatalf("decode wrong: %v", err)
	}
	if wrongBody.PrefillAddress != "" {
		t.Fatalf("wrong contract must not prefill: %#v", wrongBody)
	}
	for _, result := range wrongBody.Results {
		if result.Kind == "tx_hash" && result.Status != "mismatch" {
			t.Fatalf("expected tx mismatch, got %#v", wrongBody.Results)
		}
	}
}

func TestVerifyClaimsRejectsMalformedAndProviderFailure(t *testing.T) {
	test := newAuthHTTPTest(t)
	test.engine = gin.New()
	apiRouter.ApiRouterWithClaimOptions(test.engine, analysis.Options{
		Provider: &fixtureChainDataProvider{
			captureErr: context.DeadlineExceeded,
		},
	}, nil)
	token := ownerToken(t, test.owner.ID)

	bad := test.request(http.MethodPost, "/api/v1/claims/verify", map[string]any{
		"selectedAddress": testTargetAddress,
		"claims": []map[string]any{
			{"kind": "address", "value": "not-valid", "role": "to", "confidence": 0.9},
		},
	}, token)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad status = %d; body: %s", bad.Code, bad.Body.String())
	}
	assertErrorCode(t, bad, "invalid_claim_address")

	providerFail := test.request(http.MethodPost, "/api/v1/claims/verify", map[string]any{
		"selectedAddress": testTargetAddress,
		"claims": []map[string]any{
			{"kind": "address", "value": testTargetAddress, "role": "to", "confidence": 0.9},
		},
	}, token)
	if providerFail.Code != http.StatusBadGateway {
		t.Fatalf("provider status = %d; body: %s", providerFail.Code, providerFail.Body.String())
	}
	assertErrorCode(t, providerFail, "provider_unavailable")
}

func encodeClaimTestJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 2, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	return buf.Bytes()
}

func multipartClaimExtractNamed(
	t *testing.T, test *authHTTPTest, token string, image []byte, filename string,
) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", filename)
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	if _, err := part.Write(image); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/claims/extract", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	test.engine.ServeHTTP(response, req)
	return response
}

func multipartClaimExtract(t *testing.T, test *authHTTPTest, token string, image []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "shot.png")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	if _, err := part.Write(image); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/claims/extract", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	test.engine.ServeHTTP(response, req)
	return response
}

func assertErrorCode(t *testing.T, response *httptest.ResponseRecorder, code string) {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v body=%s", err, response.Body.String())
	}
	if body.Code != code {
		t.Fatalf("code = %q, want %q", body.Code, code)
	}
}

func assertJSONField(t *testing.T, raw []byte, key, want string) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, _ := body[key].(string)
	if got != want {
		t.Fatalf("%s = %q, want %q; body=%s", key, got, want, string(raw))
	}
}

var _ controller.ClaimExtractor = (*fixtureClaimExtractor)(nil)
