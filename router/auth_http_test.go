package router_test

import (
	"bytes"
	"chaintrace/auth"
	"chaintrace/controller"
	"chaintrace/model"
	"chaintrace/model/store"
	"chaintrace/router"
	"chaintrace/utils"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const testPassword = "correct horse battery staple"

type authHTTPTest struct {
	t               *testing.T
	engine          *gin.Engine
	owner           store.User
	verificationURL string
}

func newAuthHTTPTest(t *testing.T) *authHTTPTest {
	return newAuthHTTPTestWithOptions(t, controller.AuthOptions{})
}

func newAuthHTTPTestWithOptions(t *testing.T, options controller.AuthOptions) *authHTTPTest {
	t.Helper()

	var dialector gorm.Dialector
	if dsn := os.Getenv("CHAINTRACE_TEST_DATABASE_DSN"); dsn != "" {
		dialector = postgres.Open(dsn)
	} else {
		dialector = sqlite.Open(fmt.Sprintf("file:auth_%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", time.Now().UnixNano()))
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(
		&store.User{},
		&store.Investigation{},
		&store.ConversationMessage{},
		&store.ConversationChunk{},
	); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	model.DB = db
	t.Cleanup(func() {
		if h := model.DB.Unscoped().Delete(&store.User{}, "email LIKE ?", "%@auth-test.invalid"); h.Error != nil {
			t.Errorf("clean test users: %v", h.Error)
		}
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	salt := "test-only-salt"
	passwordHash, err := auth.P2H(testPassword + salt)
	if err != nil {
		t.Fatalf("hash test password: %v", err)
	}
	suffix := time.Now().UnixNano()
	owner := store.User{
		Username:    fmt.Sprintf("owner%d", suffix%1_000_000_000),
		DisplayName: "Test Owner",
		Role:        utils.RoleRootUser,
		Email:       fmt.Sprintf("owner%d@auth-test.invalid", suffix),
		Password:    passwordHash,
		Salt:        salt,
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create test owner: %v", err)
	}

	utils.JWTSecret = "test-only-jwt-secret-with-sufficient-length"
	utils.TokenExpireSecond = time.Hour
	if err := auth.InitAuth(); err != nil {
		t.Fatalf("initialize auth: %v", err)
	}

	gin.SetMode(gin.TestMode)
	test := &authHTTPTest{t: t, engine: gin.New(), owner: owner}
	options.SendVerification = func(_ string, _ string, verificationURL string) error {
		test.verificationURL = verificationURL
		return nil
	}
	options.FrontendBaseURL = "https://frontend.test"
	options.VerifyBaseURL = "https://backend.test"
	router.ApiRouterWithAuthOptions(test.engine, options)
	return test
}

func (a *authHTTPTest) request(method, target string, body any, token string) *httptest.ResponseRecorder {
	a.t.Helper()
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			a.t.Fatalf("encode request: %v", err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, target, requestBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	a.engine.ServeHTTP(response, req)
	return response
}

func TestOwnerCanStartLoginWithEmail(t *testing.T) {
	test := newAuthHTTPTest(t)

	response := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    test.owner.Email,
		"password": testPassword,
	}, "")

	if response.Code != http.StatusAccepted {
		t.Fatalf("login status = %d, want %d; body: %s", response.Code, http.StatusAccepted, response.Body.String())
	}
	verificationURL, err := url.Parse(test.verificationURL)
	if err != nil {
		t.Fatalf("parse verification URL: %v", err)
	}
	if got, want := verificationURL.Scheme+"://"+verificationURL.Host+verificationURL.Path, "https://backend.test/Authentication/verify"; got != want {
		t.Errorf("verification URL = %q, want %q", got, want)
	}
	for _, key := range []string{"code", "id"} {
		if strings.TrimSpace(verificationURL.Query().Get(key)) == "" {
			t.Errorf("verification URL query %q is empty: %s", key, test.verificationURL)
		}
	}
}

func TestOwnerCanStartLoginWithUsername(t *testing.T) {
	test := newAuthHTTPTest(t)

	response := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"username": test.owner.Username,
		"password": testPassword,
	}, "")

	if response.Code != http.StatusAccepted {
		t.Fatalf("login status = %d, want %d; body: %s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if test.verificationURL == "" {
		t.Fatal("verification email was not sent")
	}
}

func TestLoginDoesNotRevealWhetherOwnerExists(t *testing.T) {
	test := newAuthHTTPTest(t)

	missingOwner := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    "missing@auth-test.invalid",
		"password": "wrong password",
	}, "")
	wrongPassword := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    test.owner.Email,
		"password": "wrong password",
	}, "")

	if missingOwner.Code != http.StatusUnauthorized || wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("credential statuses = (%d, %d), want both %d", missingOwner.Code, wrongPassword.Code, http.StatusUnauthorized)
	}
	if missingOwner.Body.String() != wrongPassword.Body.String() {
		t.Errorf("credential errors differ: missing=%s wrong-password=%s", missingOwner.Body.String(), wrongPassword.Body.String())
	}
	if test.verificationURL != "" {
		t.Fatal("verification email was sent for invalid credentials")
	}
}

func TestVerificationRedirectUsesFrontendCallbackQuery(t *testing.T) {
	test := newAuthHTTPTest(t)
	login := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    test.owner.Email,
		"password": testPassword,
	}, "")
	if login.Code != http.StatusAccepted {
		t.Fatalf("login status = %d, want %d; body: %s", login.Code, http.StatusAccepted, login.Body.String())
	}

	verification := test.request(http.MethodGet, test.verificationURL, nil, "")

	if verification.Code != http.StatusFound {
		t.Fatalf("verification status = %d, want %d; body: %s", verification.Code, http.StatusFound, verification.Body.String())
	}
	callback, err := url.Parse(verification.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse callback URL: %v", err)
	}
	if got, want := callback.Scheme+"://"+callback.Host+callback.Path, "https://frontend.test/login/callback"; got != want {
		t.Errorf("callback URL = %q, want %q", got, want)
	}
	if callback.Query().Get("code") == "" {
		t.Error("callback code is empty")
	}
	if got, want := callback.Query().Get("id"), fmt.Sprint(test.owner.ID); got != want {
		t.Errorf("callback id = %q, want %q", got, want)
	}
}

func TestVerificationRejectsTamperedChallengeCode(t *testing.T) {
	test := newAuthHTTPTest(t)
	login := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    test.owner.Email,
		"password": testPassword,
	}, "")
	if login.Code != http.StatusAccepted {
		t.Fatalf("login status = %d, want %d; body: %s", login.Code, http.StatusAccepted, login.Body.String())
	}
	verificationURL, err := url.Parse(test.verificationURL)
	if err != nil {
		t.Fatalf("parse verification URL: %v", err)
	}
	query := verificationURL.Query()
	query.Set("code", query.Get("code")+"x")
	verificationURL.RawQuery = query.Encode()

	response := test.request(http.MethodGet, verificationURL.String(), nil, "")

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("tampered verification status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestVerificationRejectsExpiredChallenge(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	test := newAuthHTTPTestWithOptions(t, controller.AuthOptions{
		Now:          func() time.Time { return now },
		ChallengeTTL: time.Minute,
	})
	login := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    test.owner.Email,
		"password": testPassword,
	}, "")
	if login.Code != http.StatusAccepted {
		t.Fatalf("login status = %d, want %d; body: %s", login.Code, http.StatusAccepted, login.Body.String())
	}
	now = now.Add(2 * time.Minute)

	response := test.request(http.MethodGet, test.verificationURL, nil, "")

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired verification status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestGuessedExchangeCodeDoesNotDiscardPendingChallenge(t *testing.T) {
	test := newAuthHTTPTest(t)
	login := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    test.owner.Email,
		"password": testPassword,
	}, "")
	if login.Code != http.StatusAccepted {
		t.Fatalf("login status = %d, want %d; body: %s", login.Code, http.StatusAccepted, login.Body.String())
	}
	verification := test.request(http.MethodGet, test.verificationURL, nil, "")
	if verification.Code != http.StatusFound {
		t.Fatalf("verification status = %d, want %d", verification.Code, http.StatusFound)
	}
	callback, err := url.Parse(verification.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse callback URL: %v", err)
	}
	guessed := callback.Query()
	guessed.Set("code", "wrong-exchange-code")
	rejected := test.request(http.MethodGet, "/Authentication/challenge?"+guessed.Encode(), nil, "")
	if rejected.Code != http.StatusUnauthorized {
		t.Fatalf("guessed exchange status = %d, want %d", rejected.Code, http.StatusUnauthorized)
	}

	accepted := test.request(http.MethodGet, "/Authentication/challenge?"+callback.RawQuery, nil, "")

	if accepted.Code != http.StatusOK {
		t.Fatalf("genuine exchange status = %d, want %d; body: %s", accepted.Code, http.StatusOK, accepted.Body.String())
	}
}

func TestVerificationChallengeCanOnlyBeUsedOnce(t *testing.T) {
	test := newAuthHTTPTest(t)
	login := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    test.owner.Email,
		"password": testPassword,
	}, "")
	if login.Code != http.StatusAccepted {
		t.Fatalf("login status = %d, want %d; body: %s", login.Code, http.StatusAccepted, login.Body.String())
	}
	first := test.request(http.MethodGet, test.verificationURL, nil, "")
	if first.Code != http.StatusFound {
		t.Fatalf("first verification status = %d, want %d", first.Code, http.StatusFound)
	}

	second := test.request(http.MethodGet, test.verificationURL, nil, "")

	if second.Code != http.StatusUnauthorized {
		t.Fatalf("replayed verification status = %d, want %d", second.Code, http.StatusUnauthorized)
	}
}

func TestVerifiedOwnerCanExchangeChallengeAndReadIdentity(t *testing.T) {
	test := newAuthHTTPTest(t)
	login := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    test.owner.Email,
		"password": testPassword,
	}, "")
	if login.Code != http.StatusAccepted {
		t.Fatalf("login status = %d, want %d; body: %s", login.Code, http.StatusAccepted, login.Body.String())
	}
	verification := test.request(http.MethodGet, test.verificationURL, nil, "")
	if verification.Code != http.StatusFound {
		t.Fatalf("verification status = %d, want %d; body: %s", verification.Code, http.StatusFound, verification.Body.String())
	}
	callback, err := url.Parse(verification.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse callback URL: %v", err)
	}
	exchangeTarget := "/Authentication/challenge?" + callback.RawQuery
	exchange := test.request(http.MethodGet, exchangeTarget, nil, "")
	if exchange.Code != http.StatusOK {
		t.Fatalf("exchange status = %d, want %d; body: %s", exchange.Code, http.StatusOK, exchange.Body.String())
	}
	var exchanged struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(exchange.Body.Bytes(), &exchanged); err != nil {
		t.Fatalf("decode exchange: %v", err)
	}
	if exchanged.Token == "" {
		t.Fatal("exchange token is empty")
	}

	identity := test.request(http.MethodGet, "/api/v1/me", nil, exchanged.Token)

	if identity.Code != http.StatusOK {
		t.Fatalf("identity status = %d, want %d; body: %s", identity.Code, http.StatusOK, identity.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(identity.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode identity: %v", err)
	}
	want := map[string]any{
		"id":           float64(test.owner.ID),
		"username":     test.owner.Username,
		"display_name": test.owner.DisplayName,
		"email":        test.owner.Email,
		"role":         float64(test.owner.Role),
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("identity = %#v, want %#v", got, want)
	}
}

func TestExchangeCodeCanOnlyBeUsedOnce(t *testing.T) {
	test := newAuthHTTPTest(t)
	login := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    test.owner.Email,
		"password": testPassword,
	}, "")
	if login.Code != http.StatusAccepted {
		t.Fatalf("login status = %d, want %d; body: %s", login.Code, http.StatusAccepted, login.Body.String())
	}
	verification := test.request(http.MethodGet, test.verificationURL, nil, "")
	if verification.Code != http.StatusFound {
		t.Fatalf("verification status = %d, want %d", verification.Code, http.StatusFound)
	}
	callback, err := url.Parse(verification.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse callback URL: %v", err)
	}
	exchangeTarget := "/Authentication/challenge?" + callback.RawQuery
	first := test.request(http.MethodGet, exchangeTarget, nil, "")
	if first.Code != http.StatusOK {
		t.Fatalf("first exchange status = %d, want %d; body: %s", first.Code, http.StatusOK, first.Body.String())
	}

	second := test.request(http.MethodGet, exchangeTarget, nil, "")

	if second.Code != http.StatusUnauthorized {
		t.Fatalf("replayed exchange status = %d, want %d", second.Code, http.StatusUnauthorized)
	}
}

func TestLogoutRevokesCurrentCredential(t *testing.T) {
	test := newAuthHTTPTest(t)
	login := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    test.owner.Email,
		"password": testPassword,
	}, "")
	if login.Code != http.StatusAccepted {
		t.Fatalf("login status = %d, want %d; body: %s", login.Code, http.StatusAccepted, login.Body.String())
	}
	verification := test.request(http.MethodGet, test.verificationURL, nil, "")
	if verification.Code != http.StatusFound {
		t.Fatalf("verification status = %d, want %d", verification.Code, http.StatusFound)
	}
	callback, err := url.Parse(verification.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse callback URL: %v", err)
	}
	exchange := test.request(http.MethodGet, "/Authentication/challenge?"+callback.RawQuery, nil, "")
	if exchange.Code != http.StatusOK {
		t.Fatalf("exchange status = %d, want %d; body: %s", exchange.Code, http.StatusOK, exchange.Body.String())
	}
	var exchanged struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(exchange.Body.Bytes(), &exchanged); err != nil {
		t.Fatalf("decode exchange: %v", err)
	}

	logout := test.request(http.MethodPost, "/api/v1/logout", nil, exchanged.Token)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d; body: %s", logout.Code, http.StatusNoContent, logout.Body.String())
	}
	identity := test.request(http.MethodGet, "/api/v1/me", nil, exchanged.Token)

	if identity.Code != http.StatusUnauthorized {
		t.Fatalf("identity after logout status = %d, want %d", identity.Code, http.StatusUnauthorized)
	}
	var body map[string]any
	if err := json.Unmarshal(identity.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode unauthorized response: %v", err)
	}
	if body["code"] != "unauthorized" {
		t.Errorf("unauthorized code = %v, want unauthorized", body["code"])
	}
}

func TestProtectedRoutesRejectJWTWithoutRequiredExpiration(t *testing.T) {
	test := newAuthHTTPTest(t)
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.JWTClaims{
		UserID: test.owner.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(uint64(test.owner.ID), 10),
			Issuer:    "chaintrace",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	})
	signed, err := token.SignedString([]byte(utils.JWTSecret))
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}

	response := test.request(http.MethodGet, "/api/v1/me", nil, signed)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing-expiration JWT status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestExchangeRejectsExpiredCode(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	test := newAuthHTTPTestWithOptions(t, controller.AuthOptions{
		Now:         func() time.Time { return now },
		ExchangeTTL: time.Minute,
	})
	login := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email":    test.owner.Email,
		"password": testPassword,
	}, "")
	if login.Code != http.StatusAccepted {
		t.Fatalf("login status = %d, want %d; body: %s", login.Code, http.StatusAccepted, login.Body.String())
	}
	verification := test.request(http.MethodGet, test.verificationURL, nil, "")
	if verification.Code != http.StatusFound {
		t.Fatalf("verification status = %d, want %d", verification.Code, http.StatusFound)
	}
	callback, err := url.Parse(verification.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse callback URL: %v", err)
	}
	now = now.Add(2 * time.Minute)

	exchange := test.request(http.MethodGet, "/Authentication/challenge?"+callback.RawQuery, nil, "")

	if exchange.Code != http.StatusUnauthorized {
		t.Fatalf("expired exchange status = %d, want %d", exchange.Code, http.StatusUnauthorized)
	}
}

func TestProtectedRoutesUseStableUnauthorizedError(t *testing.T) {
	test := newAuthHTTPTest(t)
	missing := test.request(http.MethodGet, "/api/v1/me", nil, "")

	malformedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	malformedRequest.Header.Set("Authorization", "Token malformed")
	malformed := httptest.NewRecorder()
	test.engine.ServeHTTP(malformed, malformedRequest)

	if missing.Code != http.StatusUnauthorized || malformed.Code != http.StatusUnauthorized {
		t.Fatalf("protected statuses = (%d, %d), want both %d", missing.Code, malformed.Code, http.StatusUnauthorized)
	}
	if missing.Body.String() != malformed.Body.String() {
		t.Errorf("unauthorized errors differ: missing=%s malformed=%s", missing.Body.String(), malformed.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(missing.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode unauthorized response: %v", err)
	}
	if body["code"] != "unauthorized" {
		t.Errorf("unauthorized code = %v, want unauthorized", body["code"])
	}
}

func TestProtectedRoutesRejectTokenForMissingOwner(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate test JWT: %v", err)
	}
	if err := model.DB.Unscoped().Delete(&test.owner).Error; err != nil {
		t.Fatalf("delete owner: %v", err)
	}

	response := test.request(http.MethodGet, "/api/v1/me", nil, token)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing-owner JWT status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestProtectedRoutesRejectUnexpectedJWTAlgorithm(t *testing.T) {
	test := newAuthHTTPTest(t)
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, auth.JWTClaims{
		UserID: test.owner.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(uint64(test.owner.ID), 10),
			Issuer:    "chaintrace",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	})
	signed, err := token.SignedString([]byte(utils.JWTSecret))
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}

	response := test.request(http.MethodGet, "/api/v1/me", nil, signed)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected-algorithm JWT status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestProtectedRoutesRejectExpiredJWT(t *testing.T) {
	test := newAuthHTTPTest(t)
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.JWTClaims{
		UserID: test.owner.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(uint64(test.owner.ID), 10),
			Issuer:    "chaintrace",
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(now.Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-time.Hour)),
		},
	})
	signed, err := token.SignedString([]byte(utils.JWTSecret))
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}

	response := test.request(http.MethodGet, "/api/v1/me", nil, signed)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired JWT status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestLoginThrottleIsPerOwner(t *testing.T) {
	test := newAuthHTTPTest(t)
	wrong := map[string]string{"email": test.owner.Email, "password": "wrong password"}

	for attempt := 1; attempt <= 5; attempt++ {
		if got := test.request(http.MethodPost, "/Authentication/login", wrong, "").Code; got != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want %d", attempt, got, http.StatusUnauthorized)
		}
	}
	if got := test.request(http.MethodPost, "/Authentication/login", wrong, "").Code; got != http.StatusTooManyRequests {
		t.Errorf("attempt 6 status = %d, want %d", got, http.StatusTooManyRequests)
	}

	// A budget shared across Owners would let one guessing run lock everybody out.
	neighbour := store.User{
		Username: test.owner.Username + "b",
		Email:    "neighbour" + test.owner.Email,
		Password: test.owner.Password,
		Salt:     test.owner.Salt,
		Role:     utils.RoleRootUser,
	}
	if err := model.DB.Create(&neighbour).Error; err != nil {
		t.Fatalf("create neighbour owner: %v", err)
	}

	locked := test.request(http.MethodPost, "/Authentication/login", map[string]string{
		"email": neighbour.Email, "password": "wrong password",
	}, "")

	if locked.Code != http.StatusUnauthorized {
		t.Errorf("neighbour status = %d, want %d", locked.Code, http.StatusUnauthorized)
	}
}
