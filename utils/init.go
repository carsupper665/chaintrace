package utils

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

var SysLog *SysLogger

func InitLogger(logName string, maxLog int) error {
	var err error
	SysLog, err = NewSysLogger(logName, maxLog)
	if err != nil {
		return err
	}
	return nil
}

func loadSecret() error {
	SessionSecret = GetEnvString("SESSION_SECRET", "")
	HMACSecret = GetEnvString("HMAC_SECRET", "")
	JWTSecret = GetEnvString("JWT_SECRET", "")
	if !IsLocalMode() {
		for key, value := range map[string]string{
			"SESSION_SECRET": SessionSecret,
			"JWT_SECRET":     JWTSecret,
		} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s is required in production", key)
			}
		}
	} else {
		var err error
		if SessionSecret == "" {
			SessionSecret, err = SecureRandomString(48)
			if err != nil {
				return err
			}
		}
		if JWTSecret == "" {
			JWTSecret, err = SecureRandomString(48)
			if err != nil {
				return err
			}
		}
	}
	if HMACSecret == "" {
		var err error
		HMACSecret, err = SecureRandomString(48)
		if err != nil {
			return err
		}
	}
	tokenExp := GetEnvInt("TOKEN_EXPIRATION", 60*60*3)
	TokenExpireSecond = time.Duration(tokenExp) * time.Second
	return nil
}

func SetUpSMTP() {
	SMTPServer = GetEnvString("SMTP_SERVER", "")
	SMTPPort = GetEnvInt("SMTP_PORT", 587)
	SMTPSSLEnabled = GetEnvBool("SMTP_SSL_ENABLED", false)
	SMTPAccount = GetEnvString("SMTP_ACCOUNT", "")
	SMTPFrom = GetEnvString("SMTP_FROM", "")
	SMTPToken = GetEnvString("SMTP_TOKEN", "")
}

func LoadEnv() error {
	if err := godotenv.Load(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	Environment = GetEnvString("APP_ENV", "")
	DebugMode = GetEnvBool("DEBUG", false)
	DCWebHookUrl = GetEnvString("DC_WEB_HOOK", "")
	if err := loadTronGrid(); err != nil {
		return err
	}

	if err := loadSecret(); err != nil {
		return err
	}
	SetUpSMTP()

	FrontEndUrl = GetEnvString("FRONTEND_BASE_URL", "")
	if FrontEndUrl == "" && IsLocalMode() {
		FrontEndUrl = "http://localhost:3000"
	}
	PostgreDSN = GetEnvString("POSTGRES_DSN", "")
	RootUser = GetEnvString("ROOT_USER", "")
	RootUserEmail = GetEnvString("ROOT_USER_EMAIL", "")
	RootPassword = GetEnvString("ROOT_PASSWORD", "")

	if !IsLocalMode() {
		if PostgreDSN == "" {
			return fmt.Errorf("POSTGRES_DSN is required in production")
		}
		if FrontEndUrl == "" {
			return fmt.Errorf("FRONTEND_BASE_URL is required in production")
		}
	}

	TrustedProxies = nil
	for _, proxy := range strings.Split(GetEnvString("TRUSTED_PROXIES", ""), ",") {
		if proxy = strings.TrimSpace(proxy); proxy != "" {
			TrustedProxies = append(TrustedProxies, proxy)
		}
	}

	origins := GetEnvString("ALLOWED_ORIGINS", "")
	if origins == "" {
		origins = FrontEndUrl
	}
	AllowedOrigins = nil
	for _, rawOrigin := range strings.Split(origins, ",") {
		rawOrigin = strings.TrimSpace(rawOrigin)
		if rawOrigin == "" {
			continue
		}
		parsed, err := url.Parse(rawOrigin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("invalid allowed origin %q", rawOrigin)
		}
		AllowedOrigins = append(AllowedOrigins, parsed.Scheme+"://"+parsed.Host)
	}
	if !IsLocalMode() && len(AllowedOrigins) == 0 {
		return fmt.Errorf("allowed frontend origin is required in production")
	}

	return nil
}

func loadTronGrid() error {
	TronGridAPIKey = strings.TrimSpace(GetEnvString("TRONGRID_API_KEY", ""))
	TronGridBaseURL = "https://api.trongrid.io"
	if IsLocalMode() {
		TronGridBaseURL = strings.TrimRight(GetEnvString("TRONGRID_BASE_URL", TronGridBaseURL), "/")
	}
	parsedBaseURL, err := url.Parse(TronGridBaseURL)
	if TronGridAPIKey != "" && (err != nil || (parsedBaseURL.Scheme != "http" && parsedBaseURL.Scheme != "https") || parsedBaseURL.Host == "") {
		return fmt.Errorf("invalid TRONGRID_BASE_URL")
	}
	TronGridHTTPTimeout = time.Duration(boundedEnvInt("TRONGRID_HTTP_TIMEOUT_MS", 10000, 100, 120000)) * time.Millisecond
	TronGridMaxRetries = boundedEnvInt("TRONGRID_MAX_RETRIES", 2, 0, 5)
	TronGridRetryBaseDelay = time.Duration(boundedEnvInt("TRONGRID_RETRY_BASE_DELAY_MS", 200, 1, 60000)) * time.Millisecond
	TronGridMaxRetryDelay = time.Duration(boundedEnvInt("TRONGRID_MAX_RETRY_DELAY_MS", 2000, 1, 120000)) * time.Millisecond
	if TronGridMaxRetryDelay < TronGridRetryBaseDelay {
		TronGridMaxRetryDelay = TronGridRetryBaseDelay
	}
	TronGridMaxResponseBytes = int64(boundedEnvInt("TRONGRID_MAX_RESPONSE_BYTES", 4<<20, 1024, 32<<20))
	TronGridPageLimit = boundedEnvInt("TRONGRID_PAGE_LIMIT", 200, 1, 200)
	TronGridMaxPages = boundedEnvInt("TRONGRID_MAX_PAGES", 100, 1, 1000)
	return nil
}

func boundedEnvInt(key string, defaultValue, minimum, maximum int) int {
	value := GetEnvInt(key, defaultValue)
	if value < minimum || value > maximum {
		return defaultValue
	}
	return value
}
