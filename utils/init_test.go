package utils

import (
	"testing"
	"time"
)

func setCompleteProductionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("DEBUG", "false")
	t.Setenv("SESSION_SECRET", "production-session-secret")
	t.Setenv("JWT_SECRET", "production-jwt-secret")
	t.Setenv("HMAC_SECRET", "production-hmac-secret")
	t.Setenv("FRONTEND_BASE_URL", "https://frontend.example")
	t.Setenv("POSTGRES_DSN", "postgres://chaintrace:secret@db.example/chaintrace")
}

func TestLoadEnvUsesOSEnvironmentWhenDotEnvIsMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	setCompleteProductionEnv(t)

	if err := LoadEnv(); err != nil {
		t.Fatalf("LoadEnv() with complete OS environment: %v", err)
	}
	if JWTSecret != "production-jwt-secret" {
		t.Errorf("JWTSecret = %q, want OS environment value", JWTSecret)
	}
	if PostgreDSN == "" {
		t.Error("PostgreDSN is empty")
	}
}

func TestLoadEnvRejectsProductionWithoutPostgres(t *testing.T) {
	t.Chdir(t.TempDir())
	setCompleteProductionEnv(t)
	t.Setenv("POSTGRES_DSN", "")

	if err := LoadEnv(); err == nil {
		t.Fatal("LoadEnv() without production POSTGRES_DSN succeeded")
	}
}

func TestLoadEnvRejectsMissingProductionSecurityConfig(t *testing.T) {
	for _, key := range []string{"SESSION_SECRET", "JWT_SECRET", "FRONTEND_BASE_URL"} {
		t.Run(key, func(t *testing.T) {
			t.Chdir(t.TempDir())
			setCompleteProductionEnv(t)
			t.Setenv(key, "")

			if err := LoadEnv(); err == nil {
				t.Fatalf("LoadEnv() without production %s succeeded", key)
			}
		})
	}
}

func TestLoadEnvKeepsExplicitLocalModeSelfContained(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "local")
	t.Setenv("DEBUG", "false")
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("HMAC_SECRET", "")
	t.Setenv("FRONTEND_BASE_URL", "")
	t.Setenv("POSTGRES_DSN", "")

	if err := LoadEnv(); err != nil {
		t.Fatalf("LoadEnv() in explicit local mode: %v", err)
	}
	if SessionSecret == "" || JWTSecret == "" || HMACSecret == "" {
		t.Fatal("local mode did not generate ephemeral secrets")
	}
	if FrontEndUrl != "http://localhost:3000" {
		t.Errorf("local frontend URL = %q", FrontEndUrl)
	}
}

func TestLoadEnvConfiguresBoundedTronGridSettingsForLocalRecordedTests(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "test")
	t.Setenv("TRONGRID_API_KEY", "recorded-key")
	t.Setenv("TRONGRID_BASE_URL", "http://127.0.0.1:43210")
	t.Setenv("TRONGRID_HTTP_TIMEOUT_MS", "250")
	t.Setenv("TRONGRID_MAX_RETRIES", "3")
	t.Setenv("TRONGRID_RETRY_BASE_DELAY_MS", "4")
	t.Setenv("TRONGRID_MAX_RETRY_DELAY_MS", "25")
	t.Setenv("TRONGRID_MAX_RESPONSE_BYTES", "65536")
	t.Setenv("TRONGRID_PAGE_LIMIT", "123")
	t.Setenv("TRONGRID_MAX_PAGES", "17")

	if err := LoadEnv(); err != nil {
		t.Fatalf("LoadEnv() with recorded TronGrid config: %v", err)
	}
	if TronGridAPIKey != "recorded-key" || TronGridBaseURL != "http://127.0.0.1:43210" || TronGridHTTPTimeout != 250*time.Millisecond ||
		TronGridMaxRetries != 3 || TronGridRetryBaseDelay != 4*time.Millisecond || TronGridMaxRetryDelay != 25*time.Millisecond ||
		TronGridMaxResponseBytes != 65536 || TronGridPageLimit != 123 || TronGridMaxPages != 17 {
		t.Errorf("TronGrid config = key %q, URL %q, timeout %s, retries %d, delays %s/%s, bytes %d, pages %d/%d",
			TronGridAPIKey, TronGridBaseURL, TronGridHTTPTimeout, TronGridMaxRetries, TronGridRetryBaseDelay, TronGridMaxRetryDelay,
			TronGridMaxResponseBytes, TronGridPageLimit, TronGridMaxPages)
	}
}

func TestLoadEnvKeepsProductionTronGridOnMainnetDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	setCompleteProductionEnv(t)
	t.Setenv("TRONGRID_API_KEY", "production-key")
	t.Setenv("TRONGRID_BASE_URL", "http://127.0.0.1:43210")

	if err := LoadEnv(); err != nil {
		t.Fatalf("LoadEnv() production TronGrid config: %v", err)
	}
	if TronGridBaseURL != "https://api.trongrid.io" {
		t.Errorf("production TronGrid base URL = %q", TronGridBaseURL)
	}
}
