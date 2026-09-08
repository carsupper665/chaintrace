package utils

import (
	"flag"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	LogPath        = flag.String("log-dir", "./logs", "specify the log directory")
	DCWebHookUrl   string // Web Hook Url, Send Message to Discord Chat
	FrontEndUrl    string
	AllowedOrigins []string
	TrustedProxies []string
	Environment    string
	SystemName     = "ChainTrace"
)

var EmailLoginAuthServerList = []string{
	"smtp.sendcloud.net",
	"smtp.azurecomm.net",
}

var (
	DebugMode     bool
	RootUser      string
	RootUserEmail string
	RootPassword  string
)

var (
	PostgreDSN string
	SQLitePath = "DB.db?_busy_timeout=5000" // Sql Lite File Path
)

var (
	TronGridAPIKey           string
	TronGridBaseURL                = "https://api.trongrid.io"
	TronGridHTTPTimeout            = 10 * time.Second
	TronGridMaxRetries             = 2
	TronGridRetryBaseDelay         = 200 * time.Millisecond
	TronGridMaxRetryDelay          = 2 * time.Second
	TronGridMaxResponseBytes int64 = 4 << 20
	TronGridPageLimit              = 200
	TronGridMaxPages               = 100
)

var (
	HMACSecret        string
	SessionSecret     string
	JWTSecret         string
	TokenExpireSecond time.Duration
)

var (
	SMTPServer     string
	SMTPPort       int
	SMTPSSLEnabled bool
	SMTPAccount    string
	SMTPFrom       string
	SMTPToken      string
)

func getEnv(key string) (string, bool) {
	v := os.Getenv(key)
	if v == "" {
		return v, false
	}
	return v, true
}

func GetEnvString(key, def string) string {
	if v, ok := getEnv(key); ok {
		return v
	}
	return def
}

func GetEnvInt(key string, def int) int {

	if v, ok := getEnv(key); ok {
		if n, err := strconv.Atoi(v); err != nil {
			return def
		} else {
			return n
		}
	}

	return def
}

func GetEnvBool(key string, def bool) bool {
	if v, ok := getEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		return def
	}
	return def
}

func IsLocalMode() bool {
	switch strings.ToLower(strings.TrimSpace(Environment)) {
	case "local", "development", "test":
		return true
	case "":
		return DebugMode
	default:
		return false
	}
}
