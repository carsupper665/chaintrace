package router_test

import (
	"chaintrace/auth"
	"chaintrace/model"
	"chaintrace/model/store"
	apiRouter "chaintrace/router"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOwnerCanCreatePendingInvestigation(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}

	response := test.request(http.MethodPost, "/api/v1/investigations", map[string]any{
		"title":   "USDT trail",
		"ownerId": 999999,
		"network": "ETHEREUM",
		"status":  "已完成",
	}, token)

	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	id, ok := got["id"].(string)
	if !ok || len(id) < 20 {
		t.Fatalf("created id = %#v, want opaque string", got["id"])
	}
	if _, err := strconv.ParseUint(id, 10, 64); err == nil {
		t.Errorf("created id %q is a guessable integer", id)
	}
	want := map[string]any{
		"title":         "USDT trail",
		"address":       nil,
		"network":       "TRON_MAINNET",
		"status":        "待處理",
		"targetLocked":  false,
		"currentResult": nil,
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("create response %s = %#v, want %#v", key, got[key], value)
		}
	}
	if got["createdAt"] == nil || got["updatedAt"] == nil {
		t.Errorf("create response is missing timestamps: %#v", got)
	}
	if _, exists := got["ownerId"]; exists {
		t.Errorf("create response exposes ownerId: %#v", got)
	}

	var saved store.Investigation
	if err := model.DB.First(&saved, "id = ?", id).Error; err != nil {
		t.Fatalf("reload created investigation from SQL: %v", err)
	}
	if saved.OwnerID != test.owner.ID || saved.Title != "USDT trail" || saved.Network != store.NetworkTRONMainnet || saved.Status != store.InvestigationPending {
		t.Errorf("saved investigation = %#v", saved)
	}
	if saved.Address != nil || saved.TargetLocked || saved.CurrentResultID != nil {
		t.Errorf("saved investigation defaults = %#v", saved)
	}
}

func TestOwnerCanListPersistedInvestigations(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	for _, title := range []string{"First trail", "Second trail"} {
		response := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": title}, token)
		if response.Code != http.StatusCreated {
			t.Fatalf("create %q status = %d; body: %s", title, response.Code, response.Body.String())
		}
	}

	response := test.request(http.MethodGet, "/api/v1/investigations", nil, token)

	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var got []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(got) != 2 || got[0].Title != "First trail" || got[1].Title != "Second trail" {
		t.Errorf("listed investigations = %#v", got)
	}
}

func TestOwnerCanSearchInvestigationsByTitle(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	for _, title := range []string{"Exchange Alpha trail", "Unrelated wallet"} {
		response := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": title}, token)
		if response.Code != http.StatusCreated {
			t.Fatalf("create %q status = %d; body: %s", title, response.Code, response.Body.String())
		}
	}

	response := test.request(http.MethodGet, "/api/v1/investigations?query=ALPHA", nil, token)

	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var got []struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Exchange Alpha trail" {
		t.Errorf("search results = %#v", got)
	}
}

func TestOwnerCanSearchInvestigationsByTRONAddress(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	response := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{
		"title":   "Contract target",
		"address": "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj",
	}, token)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body: %s", response.Code, response.Body.String())
	}

	response = test.request(http.MethodGet, "/api/v1/investigations?query=LMEQCDJ", nil, token)

	if response.Code != http.StatusOK {
		t.Fatalf("address search status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var got []struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode address search: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Contract target" {
		t.Errorf("address search results = %#v", got)
	}
}

func TestOwnerCanReloadInvestigationDetail(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	created := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": "Durable trail"}, token)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body: %s", created.Code, created.Body.String())
	}
	var creation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &creation); err != nil {
		t.Fatalf("decode creation: %v", err)
	}

	// A fresh router simulates a browser reload while retaining only SQL state.
	test.engine = gin.New()
	apiRouter.ApiRouter(test.engine)
	response := test.request(http.MethodGet, "/api/v1/investigations/"+creation.ID, nil, token)

	if response.Code != http.StatusOK {
		t.Fatalf("detail after reload status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if got["id"] != creation.ID || got["title"] != "Durable trail" || got["currentResult"] != nil {
		t.Errorf("reloaded detail = %#v", got)
	}
}

func TestInvestigationTitleValidationHasStableError(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	var stableBody string
	for _, title := range []string{"", "   ", strings.Repeat("x", 121)} {
		response := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": title}, token)
		if response.Code != http.StatusBadRequest {
			t.Errorf("create with title length %d status = %d, want %d; body: %s", len(title), response.Code, http.StatusBadRequest, response.Body.String())
			continue
		}
		if stableBody == "" {
			stableBody = response.Body.String()
		} else if response.Body.String() != stableBody {
			t.Errorf("title validation body = %s, want stable %s", response.Body.String(), stableBody)
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode validation response: %v", err)
		}
		if body["code"] != "validation_error" {
			t.Errorf("validation code = %#v, want validation_error", body["code"])
		}
	}

	boundary := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": strings.Repeat("界", 120)}, token)
	if boundary.Code != http.StatusCreated {
		t.Errorf("create with 120-character title status = %d, want %d; body: %s", boundary.Code, http.StatusCreated, boundary.Body.String())
	}
}

func TestOwnerCanCreateInvestigationWithValidTRONTarget(t *testing.T) {
	const address = "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj"
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}

	response := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{
		"title":   "USDT contract",
		"address": address,
	}, token)

	if response.Code != http.StatusCreated {
		t.Fatalf("create with valid target status = %d, want %d; body: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	var got struct {
		ID      string  `json:"id"`
		Address *string `json:"address"`
		Network string  `json:"network"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode creation: %v", err)
	}
	if got.Address == nil || *got.Address != address || got.Network != "TRON_MAINNET" {
		t.Errorf("created target = %#v", got)
	}
	var saved store.Investigation
	if err := model.DB.First(&saved, "id = ?", got.ID).Error; err != nil {
		t.Fatalf("read target from SQL: %v", err)
	}
	if saved.Address == nil || *saved.Address != address || saved.Network != store.NetworkTRONMainnet {
		t.Errorf("saved target = %#v", saved)
	}
}

func TestInvalidTRONTargetsAreRejectedByBase58Check(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	invalidAddresses := []string{
		"TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdk", // checksum mutation
		"0xdAC17F958D2ee523a2206206994597C13D831ec7",
		"T0-not-base58",
	}
	var stableBody string
	for _, address := range invalidAddresses {
		response := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{
			"title":   "Invalid target",
			"address": address,
		}, token)
		if response.Code != http.StatusBadRequest {
			t.Errorf("create with address %q status = %d, want %d; body: %s", address, response.Code, http.StatusBadRequest, response.Body.String())
			continue
		}
		if stableBody == "" {
			stableBody = response.Body.String()
		} else if response.Body.String() != stableBody {
			t.Errorf("invalid target body = %s, want stable %s", response.Body.String(), stableBody)
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode invalid target response: %v", err)
		}
		if body["code"] != "invalid_tron_target" {
			t.Errorf("invalid target code = %#v, want invalid_tron_target", body["code"])
		}
	}
	var count int64
	if err := model.DB.Model(&store.Investigation{}).Where("owner_id = ?", test.owner.ID).Count(&count).Error; err != nil {
		t.Fatalf("count invalid target records: %v", err)
	}
	if count != 0 {
		t.Errorf("invalid targets persisted %d SQL records, want 0", count)
	}
}

func TestOwnerCanRenamePendingInvestigation(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	created := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": "Old title"}, token)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body: %s", created.Code, created.Body.String())
	}
	var creation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &creation); err != nil {
		t.Fatalf("decode creation: %v", err)
	}

	response := test.request(http.MethodPatch, "/api/v1/investigations/"+creation.ID, map[string]any{
		"title":   " Renamed trail ",
		"ownerId": 999999,
		"status":  "已完成",
	}, token)

	if response.Code != http.StatusOK {
		t.Fatalf("rename status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode rename response: %v", err)
	}
	if got["title"] != "Renamed trail" || got["status"] != "待處理" {
		t.Errorf("renamed investigation = %#v", got)
	}
	var saved store.Investigation
	if err := model.DB.First(&saved, "id = ?", creation.ID).Error; err != nil {
		t.Fatalf("read renamed investigation from SQL: %v", err)
	}
	if saved.Title != "Renamed trail" || saved.OwnerID != test.owner.ID || saved.Status != store.InvestigationPending {
		t.Errorf("saved rename = %#v", saved)
	}
	list := test.request(http.MethodGet, "/api/v1/investigations", nil, token)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "Renamed trail") || strings.Contains(list.Body.String(), "Old title") {
		t.Errorf("list after rename status = %d; body: %s", list.Code, list.Body.String())
	}
}

func TestOwnerCanUpdateUnlockedPendingTRONTarget(t *testing.T) {
	const firstAddress = "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj"
	const secondAddress = "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	created := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": "Editable target"}, token)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body: %s", created.Code, created.Body.String())
	}
	var creation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &creation); err != nil {
		t.Fatalf("decode creation: %v", err)
	}

	for _, address := range []string{firstAddress, secondAddress} {
		response := test.request(http.MethodPatch, "/api/v1/investigations/"+creation.ID, map[string]string{"address": address}, token)
		if response.Code != http.StatusOK {
			t.Fatalf("update target to %q status = %d, want %d; body: %s", address, response.Code, http.StatusOK, response.Body.String())
		}
		var got struct {
			Address      *string `json:"address"`
			Network      string  `json:"network"`
			Status       string  `json:"status"`
			TargetLocked bool    `json:"targetLocked"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode target update: %v", err)
		}
		if got.Address == nil || *got.Address != address || got.Network != "TRON_MAINNET" || got.Status != "待處理" || got.TargetLocked {
			t.Errorf("updated target = %#v", got)
		}
	}
	var saved store.Investigation
	if err := model.DB.First(&saved, "id = ?", creation.ID).Error; err != nil {
		t.Fatalf("read updated target from SQL: %v", err)
	}
	if saved.Address == nil || *saved.Address != secondAddress {
		t.Errorf("saved updated target = %#v", saved)
	}

	cleared := test.request(http.MethodPatch, "/api/v1/investigations/"+creation.ID, map[string]string{"address": ""}, token)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear target status = %d, want %d; body: %s", cleared.Code, http.StatusOK, cleared.Body.String())
	}
	var clearedDTO struct {
		Address *string `json:"address"`
	}
	if err := json.Unmarshal(cleared.Body.Bytes(), &clearedDTO); err != nil {
		t.Fatalf("decode cleared target: %v", err)
	}
	if clearedDTO.Address != nil {
		t.Errorf("cleared target address = %q, want null", *clearedDTO.Address)
	}
	var clearedRecord store.Investigation
	if err := model.DB.First(&clearedRecord, "id = ?", creation.ID).Error; err != nil {
		t.Fatalf("read cleared target from SQL: %v", err)
	}
	if clearedRecord.Address != nil {
		t.Errorf("cleared SQL target address = %q, want NULL", *clearedRecord.Address)
	}
}

func TestOwnerCanDeletePendingInvestigation(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	created := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": "Delete me"}, token)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body: %s", created.Code, created.Body.String())
	}
	var creation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &creation); err != nil {
		t.Fatalf("decode creation: %v", err)
	}

	response := test.request(http.MethodDelete, "/api/v1/investigations/"+creation.ID, nil, token)

	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body: %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	detail := test.request(http.MethodGet, "/api/v1/investigations/"+creation.ID, nil, token)
	if detail.Code != http.StatusNotFound {
		t.Errorf("deleted detail status = %d, want %d; body: %s", detail.Code, http.StatusNotFound, detail.Body.String())
	}
	list := test.request(http.MethodGet, "/api/v1/investigations", nil, token)
	if list.Code != http.StatusOK || strings.TrimSpace(list.Body.String()) != "[]" {
		t.Errorf("list after delete status = %d; body: %s", list.Code, list.Body.String())
	}
	var count int64
	if err := model.DB.Model(&store.Investigation{}).Where("id = ?", creation.ID).Count(&count).Error; err != nil {
		t.Fatalf("count deleted SQL record: %v", err)
	}
	if count != 0 {
		t.Errorf("deleted investigation SQL count = %d, want 0", count)
	}
}

func TestInvestigationRoutesRequireAuthentication(t *testing.T) {
	test := newAuthHTTPTest(t)
	requests := []struct {
		method string
		target string
		body   any
	}{
		{method: http.MethodGet, target: "/api/v1/investigations"},
		{method: http.MethodPost, target: "/api/v1/investigations", body: map[string]string{"title": "Unauthorized"}},
		{method: http.MethodGet, target: "/api/v1/investigations/missing"},
		{method: http.MethodPatch, target: "/api/v1/investigations/missing", body: map[string]string{"title": "Unauthorized"}},
		{method: http.MethodDelete, target: "/api/v1/investigations/missing"},
		{method: http.MethodPost, target: "/api/v1/investigations/missing/analysis-runs"},
		{method: http.MethodGet, target: "/api/v1/investigations/missing/analysis-runs/missing"},
		{method: http.MethodDelete, target: "/api/v1/investigations/missing/analysis-runs/missing"},
		{method: http.MethodGet, target: "/api/v1/investigations/missing/current-result"},
	}
	var stableBody string
	for _, request := range requests {
		response := test.request(request.method, request.target, request.body, "")
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want %d; body: %s", request.method, request.target, response.Code, http.StatusUnauthorized, response.Body.String())
			continue
		}
		if stableBody == "" {
			stableBody = response.Body.String()
		} else if response.Body.String() != stableBody {
			t.Errorf("%s %s unauthorized body = %s, want stable %s", request.method, request.target, response.Body.String(), stableBody)
		}
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(stableBody), &body); err != nil {
		t.Fatalf("decode unauthorized response: %v", err)
	}
	if body["code"] != "unauthorized" {
		t.Errorf("unauthorized code = %#v, want unauthorized", body["code"])
	}
}

func TestInvestigationsAreIsolatedBetweenOwners(t *testing.T) {
	test := newAuthHTTPTest(t)
	suffix := strconv.FormatUint(uint64(test.owner.ID), 10)
	secondOwner := store.User{
		Username: "isolated" + suffix,
		Email:    "isolated" + suffix + "@auth-test.invalid",
		Password: "test-only",
		Salt:     "test-only",
	}
	if err := model.DB.Create(&secondOwner).Error; err != nil {
		t.Fatalf("create second owner: %v", err)
	}
	firstToken, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate first owner token: %v", err)
	}
	secondToken, err := auth.GenJWT(secondOwner.ID, "")
	if err != nil {
		t.Fatalf("generate second owner token: %v", err)
	}

	firstCreation := test.request(http.MethodPost, "/api/v1/investigations", map[string]any{
		"title":   "First owner secret",
		"ownerId": secondOwner.ID,
	}, firstToken)
	secondCreation := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": "Second owner trail"}, secondToken)
	if firstCreation.Code != http.StatusCreated || secondCreation.Code != http.StatusCreated {
		t.Fatalf("owner create statuses = (%d, %d); bodies: %s / %s", firstCreation.Code, secondCreation.Code, firstCreation.Body.String(), secondCreation.Body.String())
	}
	var first struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(firstCreation.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first creation: %v", err)
	}

	secondList := test.request(http.MethodGet, "/api/v1/investigations", nil, secondToken)
	if secondList.Code != http.StatusOK || !strings.Contains(secondList.Body.String(), "Second owner trail") || strings.Contains(secondList.Body.String(), "First owner secret") {
		t.Errorf("second owner list status = %d; body: %s", secondList.Code, secondList.Body.String())
	}
	secondSearch := test.request(http.MethodGet, "/api/v1/investigations?query=First%20owner%20secret", nil, secondToken)
	if secondSearch.Code != http.StatusOK || strings.TrimSpace(secondSearch.Body.String()) != "[]" {
		t.Errorf("second owner cross-search status = %d; body: %s", secondSearch.Code, secondSearch.Body.String())
	}

	missingID := "missingInvestigation00000"
	operations := []struct {
		method string
		body   any
	}{
		{method: http.MethodGet},
		{method: http.MethodPatch, body: map[string]string{"title": "Stolen"}},
		{method: http.MethodDelete},
	}
	for _, operation := range operations {
		crossOwner := test.request(operation.method, "/api/v1/investigations/"+first.ID, operation.body, secondToken)
		missing := test.request(operation.method, "/api/v1/investigations/"+missingID, operation.body, secondToken)
		if crossOwner.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
			t.Errorf("%s cross/missing statuses = (%d, %d), want both %d", operation.method, crossOwner.Code, missing.Code, http.StatusNotFound)
		}
		if crossOwner.Body.String() != missing.Body.String() {
			t.Errorf("%s enumerates ownership: cross=%s missing=%s", operation.method, crossOwner.Body.String(), missing.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(crossOwner.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s not-found response: %v", operation.method, err)
		}
		if body["code"] != "investigation_not_found" {
			t.Errorf("%s not-found code = %#v", operation.method, body["code"])
		}
	}

	var saved store.Investigation
	if err := model.DB.First(&saved, "id = ?", first.ID).Error; err != nil {
		t.Fatalf("read first owner's SQL record: %v", err)
	}
	if saved.OwnerID != test.owner.ID || saved.Title != "First owner secret" {
		t.Errorf("first owner's record was spoofed or mutated: %#v", saved)
	}
	for _, ownerID := range []uint{test.owner.ID, secondOwner.ID} {
		var count int64
		if err := model.DB.Model(&store.Investigation{}).Where("owner_id = ?", ownerID).Count(&count).Error; err != nil {
			t.Fatalf("count owner %d records: %v", ownerID, err)
		}
		if count != 1 {
			t.Errorf("owner %d SQL record count = %d, want 1", ownerID, count)
		}
	}
}

func TestInvalidInvestigationPatchDoesNotMutateSQL(t *testing.T) {
	const address = "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj"
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	created := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": "Stable title", "address": address}, token)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body: %s", created.Code, created.Body.String())
	}
	var creation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &creation); err != nil {
		t.Fatalf("decode creation: %v", err)
	}

	for _, title := range []string{" ", strings.Repeat("x", 121)} {
		response := test.request(http.MethodPatch, "/api/v1/investigations/"+creation.ID, map[string]string{"title": title}, token)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"validation_error"`) {
			t.Errorf("invalid title patch status = %d; body: %s", response.Code, response.Body.String())
		}
	}
	invalidTarget := test.request(http.MethodPatch, "/api/v1/investigations/"+creation.ID, map[string]string{
		"address": "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdk",
	}, token)
	if invalidTarget.Code != http.StatusBadRequest || !strings.Contains(invalidTarget.Body.String(), `"code":"invalid_tron_target"`) {
		t.Errorf("invalid target patch status = %d; body: %s", invalidTarget.Code, invalidTarget.Body.String())
	}

	var saved store.Investigation
	if err := model.DB.First(&saved, "id = ?", creation.ID).Error; err != nil {
		t.Fatalf("read investigation after invalid patches: %v", err)
	}
	if saved.Title != "Stable title" || saved.Address == nil || *saved.Address != address {
		t.Errorf("invalid patch mutated SQL record: %#v", saved)
	}
}

func TestLockedInvestigationTargetCannotChange(t *testing.T) {
	const originalAddress = "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj"
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	created := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": "Locked", "address": originalAddress}, token)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body: %s", created.Code, created.Body.String())
	}
	var creation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &creation); err != nil {
		t.Fatalf("decode creation: %v", err)
	}
	if err := model.DB.Model(&store.Investigation{}).Where("id = ?", creation.ID).Updates(map[string]any{
		"status":        store.InvestigationCompleted,
		"target_locked": true,
	}).Error; err != nil {
		t.Fatalf("lock SQL target: %v", err)
	}

	targetUpdate := test.request(http.MethodPatch, "/api/v1/investigations/"+creation.ID, map[string]string{
		"address": "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb",
	}, token)
	if targetUpdate.Code != http.StatusConflict || !strings.Contains(targetUpdate.Body.String(), `"code":"immutable_investigation_target"`) {
		t.Errorf("locked target update status = %d, want %d; body: %s", targetUpdate.Code, http.StatusConflict, targetUpdate.Body.String())
	}
	rename := test.request(http.MethodPatch, "/api/v1/investigations/"+creation.ID, map[string]string{"title": "Still renameable"}, token)
	if rename.Code != http.StatusOK {
		t.Errorf("locked investigation rename status = %d, want %d; body: %s", rename.Code, http.StatusOK, rename.Body.String())
	}

	var saved store.Investigation
	if err := model.DB.First(&saved, "id = ?", creation.ID).Error; err != nil {
		t.Fatalf("read locked investigation: %v", err)
	}
	if saved.Address == nil || *saved.Address != originalAddress || saved.Title != "Still renameable" || !saved.TargetLocked {
		t.Errorf("locked investigation after patches = %#v", saved)
	}
}
