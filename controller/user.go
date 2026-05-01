package controller

import (
	"chaintrace/auth"
	"chaintrace/model"
	"chaintrace/model/store"
	"chaintrace/utils"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

type loginRequest struct {
	Email    string `json:"email" binding:"omitempty,email"`
	Username string `json:"username" binding:"omitempty"`
	Password string `json:"password" binding:"required"`
}

type VerifyLoginCache struct {
	Code     string
	Email    string
	Exp      time.Duration
	CreateAt time.Time
}

type VerifyTokenCache struct {
	AuthToken string
	UserEmail string
	Exp       time.Duration
	CreateAt  time.Time
}

type EmailChallengeStore struct {
	cache      map[string]VerifyLoginCache
	tokenCache map[string]VerifyTokenCache
	Mu         sync.RWMutex
}

func NewChallengeStore() *EmailChallengeStore {
	return &EmailChallengeStore{
		cache:      make(map[string]VerifyLoginCache),
		tokenCache: make(map[string]VerifyTokenCache),
	}
}

func (s *EmailChallengeStore) get(id string) *VerifyLoginCache {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	v, _ := s.cache[id]
	return &v
}

func (s *EmailChallengeStore) DelById(id string) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	_, ok := s.cache[id]
	if !ok {
		return
	}
	delete(s.cache, id)
}

func (s *EmailChallengeStore) getEmail(id string) (string, bool) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	v, ok := s.cache[id]
	if !ok {
		return "", false
	}

	return v.Email, true
}

func (s *EmailChallengeStore) valid(id string, inpCode string, hashEmail string) bool {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	v, ok := s.cache[id]
	if !ok {
		return false
	}

	ca := v.CreateAt
	expired := time.Now().After(ca.Add(v.Exp))
	isCode := inpCode == v.Email
	validHash := auth.VP(hashEmail, v.Email)
	if expired && !isCode && !validHash {
		return false
	}
	delete(s.cache, id)

	return true
}

func (s *EmailChallengeStore) CreateLoginChallenge(id, email string) string {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if v, ok := s.cache[id]; ok {
		isExpired := time.Now().After(v.CreateAt.Add(v.Exp))
		if !isExpired {
			return v.Code
		}
		delete(s.cache, id)
	}

	code := utils.GetRandomIntString(16)
	s.cache[id] = VerifyLoginCache{
		Code:     code,
		Email:    email,
		CreateAt: time.Now(),
		Exp:      5 * time.Minute,
	}

	return code
}

func (s *EmailChallengeStore) setVerifyToken(id, code, email string) {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	s.tokenCache[id] = VerifyTokenCache{
		AuthToken: code,
		UserEmail: email,
		Exp:       3 * time.Minute,
		CreateAt:  time.Now(),
	}
}

func (s *EmailChallengeStore) UrlVerifyLogin(c *gin.Context) {
	//clientIP := c.ClientIP() // for login Attempt Record

	code := c.Query("code")
	hashEmail := c.Query("eh")
	id := c.Query("id")

	email, ok := s.getEmail(id)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not found"})
		return
	}
	isValid := s.valid(id, code, hashEmail)

	if !isValid {

		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired verification code"})
		return
	}

	frontend := utils.GetEnvString("FRONTEND_BASE_URL", "http://localhost:3000")
	authCode := utils.GetRandomString(32)
	uri := fmt.Sprintf("%s/login/callback?code=%s?=id%s", frontend, authCode, id)

	s.setVerifyToken(id, authCode, email)

	c.Redirect(http.StatusFound, uri)
	return
}

const (
	serverErr int8 = 1
	authErr   int8 = 0
	nilErr    int8 = 99
)

func (s *EmailChallengeStore) challenge(id, code, ip string) (string, int8, error) {
	s.Mu.Lock()

	v, ok := s.tokenCache[id]
	if !ok {
		s.Mu.Unlock()
		return "", authErr, fmt.Errorf("not found")
	}
	ca := v.CreateAt
	expired := time.Now().After(ca.Add(v.Exp))
	if expired {
		delete(s.tokenCache, id)
		s.Mu.Unlock()
		return "", authErr, fmt.Errorf("expired")
	}
	if code != v.AuthToken {
		delete(s.tokenCache, id)
		s.Mu.Unlock()
		return "", authErr, fmt.Errorf("invalid verification code")
	}
	logger.Debugf("Verification code valid for id: %s, email: %s", id, v.UserEmail)
	email := v.UserEmail
	delete(s.tokenCache, id)
	s.Mu.Unlock()

	user, err := model.GetUserByEmail(email)
	if err != nil {
		return "", serverErr, err
	}

	token, err := auth.GenJWT(user.ID, ip)
	if err != nil {
		return "", serverErr, err
	}

	return token, nilErr, nil
}

func (s *EmailChallengeStore) ExchangeToken(c *gin.Context) {
	code := c.Query("code")
	id := c.Query("id")
	ip := c.ClientIP()
	token, errCode, err := s.challenge(id, code, ip)
	if err != nil {
		if errCode == serverErr {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
	return
}

func (s *EmailChallengeStore) ChallengeLogin(c *gin.Context) {
	//clientIP := c.ClientIP()
	var req loginRequest
	var user *store.User
	var err error
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	switch {
	case req.Email != "":
		user, err = model.GetUserByEmail(req.Email)
	case req.Username != "":
		user, err = model.GetUserByEmail(req.Username)
	default:
		c.JSON(400, gin.H{"error": "Email or Username is required"})
		return
	}

	if err != nil {
		if err.Error() == "record not found" {
			c.JSON(401, gin.H{"error": "User not found"})
		} else {
			c.JSON(500, gin.H{"error": "Internal server error"})
		}
		return
	}

	v := auth.VP(req.Password+user.Salt, user.Password)
	if !v {
		c.JSON(401, gin.H{"error": "Invalid password"})
		return
	}

	if err := s.VerificationEmail(c, user.Email, fmt.Sprint(user.ID), user.Username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send verification email"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Send Email For new device."})
	return
}

func (s *EmailChallengeStore) VerificationEmail(c *gin.Context, email, id, username string) error {
	hashEmail, err := auth.P2H(email)
	if err != nil {
		return err
	}
	code := s.CreateLoginChallenge(id, email)
	port := utils.GetEnvString("PORT", "3000")
	defaultUrl := utils.GetEnvString("LOGIN_VERIFY_BASE_URL", "http://localhost:"+port)
	url := fmt.Sprintf("%s/Authentication/verify?code=%s&eh=%s&id=%s", defaultUrl, code, hashEmail, id)
	htmlMsg := fmt.Sprintf(
		`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Verification Link</title>
  <style>
    body {
      margin: 0;
      padding: 24px 12px;
      font-family: "Segoe UI", "Noto Sans TC", Arial, sans-serif;
      line-height: 1.6;
      background: radial-gradient(circle at center, #1a1a20 0%%, #0a0a0c 100%%);
      color: #e5e7eb;
    }
    .container {
      max-width: 620px;
      margin: 0 auto;
      padding: 24px;
      border-radius: 10px;
      background: rgba(20, 20, 25, 0.92);
      border: 1px solid #333333;
      box-shadow: 0 0 24px rgba(0, 0, 0, 0.45), inset 0 0 16px rgba(24, 160, 88, 0.08);
    }
    .title {
      margin: 0 0 14px 0;
      font-size: 20px;
      color: #18a058;
      letter-spacing: 0.4px;
      font-weight: 700;
    }
    p {
      margin: 10px 0;
      color: #cbd5e1;
    }
    .verify-btn {
      display: inline-block;
      margin: 10px 0 4px 0;
      padding: 10px 16px;
      border-radius: 6px;
      border: 1px solid #18a058;
      background: #18a058;
      color: #ffffff !important;
      text-decoration: none;
      font-weight: 700;
      letter-spacing: 0.4px;
    }
    .muted {
      color: #9ca3af;
      font-size: 13px;
      margin-top: 14px;
    }
    .mono {
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 12px;
      color: #cbd5e1;
      word-break: break-all;
      background: rgba(0, 0, 0, 0.25);
      border: 1px solid #2b2b2b;
      padding: 10px;
      border-radius: 6px;
    }
  </style>
</head>
<body>
  <div class="container">
    <h1 class="title">MC-SERVER Verification</h1>
    <p>Hello %s,</p>
    <p>Click the button below to verify your login:</p>

    <p>
      <a class="verify-btn" href="%s" target="_blank" rel="noopener noreferrer">
        VERIFY LOGIN
      </a>
    </p>

    <p>This link will expire in <strong>5 minutes</strong>.</p>

    <p class="muted">If the button does not work, copy this URL:</p>
    <p class="mono">%s</p>

    <p>If you did not request this, please ignore this email.</p>
    <p>Thank you,<br>The %s Team</p>
  </div>
</body>
</html>`,
		username,
		url,
		url,
		"chainTrace",
	)

	err = utils.SendEmail(
		"Login Verification Code",
		email, // 使用者的 email
		htmlMsg,
	)

	if err != nil {
		utils.SysLog.Errorf("Login Verification Code failed: %v, User: %s, Request ID: %s", err, username, c.Request.Context().Value(utils.RequestIdKey))
	}

	return err
}

type RegReq struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=72"`
}

func RegisterNewUser(c *gin.Context) {
	var req RegReq

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request"})
		return
	}

	if ok := model.IsExists(req.Username); ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "User already exists"})
		return
	}

	if ok := model.IsEmailExist(req.Email); ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Email already exists"})
		return
	}

	salt := utils.GetRandomString(16)
	hashPassword, err := auth.P2H(req.Password + salt)
	requestId := c.Request.Context().Value(utils.RequestIdKey)

	if err != nil {
		utils.SysLog.Errorf("Register new user failed: %v, Request ID: %s", err, requestId)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Fail to Create User", "request_id": requestId})
		return
	}
	NewUser := &store.User{
		Username: req.Username,
		Email:    req.Email,
		Password: hashPassword,
		Salt:     salt,
	}

	if err := model.AddUser(NewUser); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Fail to Create User", "request_id": requestId})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Success Create User"})
	return
}
