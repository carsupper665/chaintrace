package controller

import (
	"chaintrace/auth"
	"chaintrace/middleware"
	"chaintrace/model"
	"chaintrace/model/store"
	"chaintrace/utils"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
	cache               map[string]VerifyLoginCache
	tokenCache          map[string]VerifyTokenCache
	Mu                  sync.RWMutex
	sendVerification    func(email, username, verificationURL string) error
	frontendBaseURL     string
	verificationBaseURL string
	now                 func() time.Time
	challengeTTL        time.Duration
	exchangeTTL         time.Duration
	// Password guessing is budgeted per Owner. Every login reaches this API from the
	// frontend BFF, so the shared per-IP budget throttles the whole deployment instead
	// of the account actually under attack.
	loginAttempts *middleware.RateLimiter
}

const (
	loginAttemptsPerWindow = 5
	loginAttemptWindow     = int64(15 * 60)
)

type AuthOptions struct {
	SendVerification func(email, username, verificationURL string) error
	FrontendBaseURL  string
	VerifyBaseURL    string
	Now              func() time.Time
	ChallengeTTL     time.Duration
	ExchangeTTL      time.Duration
}

func NewChallengeStore(options ...AuthOptions) *EmailChallengeStore {
	option := AuthOptions{}
	if len(options) > 0 {
		option = options[0]
	}
	if option.SendVerification == nil {
		option.SendVerification = sendVerificationEmail
	}
	if option.FrontendBaseURL == "" {
		option.FrontendBaseURL = utils.FrontEndUrl
	}
	if option.VerifyBaseURL == "" {
		port := utils.GetEnvString("PORT", "3000")
		option.VerifyBaseURL = utils.GetEnvString("LOGIN_VERIFY_BASE_URL", "http://localhost:"+port)
	}
	if option.Now == nil {
		option.Now = time.Now
	}
	if option.ChallengeTTL == 0 {
		option.ChallengeTTL = 5 * time.Minute
	}
	if option.ExchangeTTL == 0 {
		option.ExchangeTTL = 3 * time.Minute
	}
	loginAttempts := &middleware.RateLimiter{}
	loginAttempts.Init(time.Duration(loginAttemptWindow) * time.Second)
	return &EmailChallengeStore{
		loginAttempts:       loginAttempts,
		cache:               make(map[string]VerifyLoginCache),
		tokenCache:          make(map[string]VerifyTokenCache),
		sendVerification:    option.SendVerification,
		frontendBaseURL:     option.FrontendBaseURL,
		verificationBaseURL: option.VerifyBaseURL,
		now:                 option.Now,
		challengeTTL:        option.ChallengeTTL,
		exchangeTTL:         option.ExchangeTTL,
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

func (s *EmailChallengeStore) valid(id string, inpCode string) bool {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	v, ok := s.cache[id]
	if !ok {
		return false
	}

	ca := v.CreateAt
	expired := s.now().After(ca.Add(v.Exp))
	isCode := subtle.ConstantTimeCompare([]byte(inpCode), []byte(v.Code)) == 1
	if expired || !isCode {
		return false
	}
	delete(s.cache, id)

	return true
}

func (s *EmailChallengeStore) CreateLoginChallenge(id, email string) (string, error) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if v, ok := s.cache[id]; ok {
		isExpired := s.now().After(v.CreateAt.Add(v.Exp))
		if !isExpired {
			return v.Code, nil
		}
		delete(s.cache, id)
	}

	code, err := utils.SecureRandomIntString(16)
	if err != nil {
		return "", err
	}
	s.cache[id] = VerifyLoginCache{
		Code:     code,
		Email:    email,
		CreateAt: s.now(),
		Exp:      s.challengeTTL,
	}

	return code, nil
}

func (s *EmailChallengeStore) setVerifyToken(id, code, email string) {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	s.tokenCache[id] = VerifyTokenCache{
		AuthToken: code,
		UserEmail: email,
		Exp:       s.exchangeTTL,
		CreateAt:  s.now(),
	}
}

func (s *EmailChallengeStore) UrlVerifyLogin(c *gin.Context) {
	//clientIP := c.ClientIP() // for login Attempt Record

	code := c.Query("code")
	id := c.Query("id")

	email, ok := s.getEmail(id)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not found"})
		return
	}
	isValid := s.valid(id, code)

	if !isValid {

		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired verification code"})
		return
	}

	frontend := s.frontendBaseURL
	authCode, err := utils.SecureRandomString(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		return
	}
	callbackURL, err := url.Parse(strings.TrimRight(frontend, "/") + "/login/callback")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		return
	}
	query := callbackURL.Query()
	query.Set("code", authCode)
	query.Set("id", id)
	callbackURL.RawQuery = query.Encode()

	s.setVerifyToken(id, authCode, email)

	c.Redirect(http.StatusFound, callbackURL.String())
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
	expired := s.now().After(ca.Add(v.Exp))
	if expired {
		delete(s.tokenCache, id)
		s.Mu.Unlock()
		return "", authErr, fmt.Errorf("expired")
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(v.AuthToken)) != 1 {
		s.Mu.Unlock()
		return "", authErr, fmt.Errorf("invalid verification code")
	}
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
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
			return
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
		user, err = model.GetUserByUsername(req.Username)
	default:
		c.JSON(400, gin.H{"error": "Email or Username is required"})
		return
	}

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"code": "invalid_credentials", "message": "Invalid credentials"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		}
		return
	}

	if !s.loginAttempts.Request("login:"+strconv.FormatUint(uint64(user.ID), 10), loginAttemptsPerWindow, loginAttemptWindow) {
		c.JSON(http.StatusTooManyRequests, gin.H{"code": "rate_limited", "message": "Too many requests"})
		return
	}

	v := auth.VP(user.Password, req.Password+user.Salt)
	if !v {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "invalid_credentials", "message": "Invalid credentials"})
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
	code, err := s.CreateLoginChallenge(id, email)
	if err != nil {
		return err
	}
	verificationURL, err := url.Parse(strings.TrimRight(s.verificationBaseURL, "/") + "/Authentication/verify")
	if err != nil {
		return err
	}
	query := verificationURL.Query()
	query.Set("code", code)
	query.Set("id", id)
	verificationURL.RawQuery = query.Encode()
	err = s.sendVerification(email, username, verificationURL.String())

	if err != nil && utils.SysLog != nil {
		utils.SysLog.Errorf("Login Verification Code failed: %v, User: %s, Request ID: %s", err, username, c.Request.Context().Value(utils.RequestIdKey))
	}

	return err
}

func sendVerificationEmail(email, username, verificationURL string) error {
	htmlMsg := fmt.Sprintf(
		`<p>Hello %s,</p><p><a href="%s">Verify login</a></p><p>%s</p>`,
		username,
		verificationURL,
		verificationURL,
	)
	return utils.SendEmail("Login Verification Code", email, htmlMsg)
}

func CurrentUser(c *gin.Context) {
	user, err := model.GetUserByID(c.GetUint("user_id"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "unauthorized", "message": "Unauthorized"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":           user.ID,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"email":        user.Email,
		"role":         user.Role,
	})
}

func Logout(c *gin.Context) {
	token := c.GetString("auth_token")
	userID := c.GetUint("user_id")
	auth.RTS.Add(token, strconv.FormatUint(uint64(userID), 10), time.Now().Add(utils.TokenExpireSecond))
	if err := auth.RTS.ClearEvent(); err != nil && utils.SysLog != nil {
		utils.SysLog.Errorf("failed to prune revoked token registry: %v", err)
	}
	c.Status(http.StatusNoContent)
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

	salt, err := utils.SecureRandomString(16)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Fail to Create User"})
		return
	}
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
