package utils

import (
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

func loadSecret() {
	SessionSecret = GetEnvString("SESSION_SECRET", "")
	if SessionSecret == "" {
		SysLog.Warn("SESSION_SECRET is empty")
		SessionSecret = "123456789asdqwe"
	}
	HMACSecret = GetEnvString("HMAC_SECRET", "")
	if HMACSecret == "" {
		SysLog.Warn("HMAC_SECRET is empty")
		HMACSecret = SessionSecret
	}
	JWTSecret = GetEnvString("JWT_SECRET", "")
	if JWTSecret == "" {
		SysLog.Warn("JWT_SECRET is empty")
		JWTSecret = SessionSecret
	}
	tokenExp := GetEnvInt("TOKEN_EXPIRATION", 60*60*3)
	TokenExpireSecond = time.Duration(tokenExp) * time.Second
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
	if err := godotenv.Load(".env"); err != nil {
		return err
	}

	DebugMode = GetEnvBool("DEBUG", false)
	DCWebHookUrl = GetEnvString("DC_WEB_HOOK", "")

	loadSecret()
	SetUpSMTP()

	FrontEndUrl = GetEnvString("FRONTEND_BASE_URL", "http://localhost:3000")
	PostgreDSN = GetEnvString("POSTGRES_DSN", "")
	RootUser = GetEnvString("ROOT_USER", "")
	RootPassword = GetEnvString("ROOT_PASSWORD", "123")

	return nil
}
