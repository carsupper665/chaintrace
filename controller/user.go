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
	"html"
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
	sendVerification    func(email, username, verificationURL string, validFor time.Duration) error
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
	// SendVerification receives the challenge's own lifetime so the email can
	// state an expiry that is true by construction rather than a number copied
	// into the copy and left to drift from ChallengeTTL.
	SendVerification func(email, username, verificationURL string, validFor time.Duration) error
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
	err = s.sendVerification(email, username, verificationURL.String(), s.challengeTTL)

	if err != nil && utils.SysLog != nil {
		utils.SysLog.Errorf("Login Verification Code failed: %v, User: %s, Request ID: %s", err, username, c.Request.Context().Value(utils.RequestIdKey))
	}

	return err
}

// Email clients are not browsers: Outlook renders through Word, so the layout
// is tables with inline styles, flex and grid do nothing, and an external
// stylesheet is dropped. Images are blocked by default, so the wordmark is
// text. The raw URL is repeated as selectable text because clients rewrite or
// strip the button often enough that a link-only email locks people out.
func sendVerificationEmail(email, username, verificationURL string, validFor time.Duration) error {
	subject, body := verificationEmail(username, verificationURL, validFor)
	return utils.SendEmail(subject, email, body)
}

// verificationEmail builds the subject and body. It is separate from sending so
// the copy can be tested without SMTP — the Owner's username reaches HTML here,
// so the escaping is worth pinning down.
func verificationEmail(username, verificationURL string, validFor time.Duration) (string, string) {
	minutes := int(validFor.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	safeUsername := html.EscapeString(username)
	safeURL := html.EscapeString(verificationURL)

	body := fmt.Sprintf(`<table width="100%%" cellpadding="0" cellspacing="0" style="max-width:600px;margin:0 auto;border-collapse:collapse;font-family:-apple-system,'Segoe UI','Noto Sans TC',sans-serif;">
<tr><td style="padding:0 0 12px 2px;">
<span style="font-size:15px;font-weight:500;color:#0f2942;letter-spacing:.02em;">ChainTrace</span>
<span style="font-size:12px;color:#8a9199;padding-left:8px;">區塊鏈調查平台</span>
</td></tr>
<tr><td style="background:#ffffff;border:1px solid #e3e6ea;border-radius:10px;padding:28px 30px;">
<p style="margin:0 0 14px;font-size:16px;color:#16191d;">%s，你好</p>
<p style="margin:0 0 22px;font-size:14px;line-height:1.7;color:#454b52;">有人要用這個信箱登入 ChainTrace。是你的話，按下面的按鈕完成登入。</p>
<table cellpadding="0" cellspacing="0" style="margin:0 0 16px;"><tr><td style="background:#0f2942;border-radius:6px;">
<a href="%s" style="display:inline-block;padding:13px 30px;font-size:15px;color:#ffffff;text-decoration:none;">確認登入</a>
</td></tr></table>
<p style="margin:0 0 22px;font-size:13px;color:#6b7280;">這個連結 <strong style="color:#16191d;font-weight:500;">%d 分鐘後失效</strong>，而且只能用一次。</p>
<div style="border-top:1px solid #eceef1;padding-top:18px;">
<p style="margin:0 0 8px;font-size:13px;color:#6b7280;">按鈕沒反應？把這段網址貼進瀏覽器：</p>
<div style="background:#f6f7f9;border:1px solid #e3e6ea;border-radius:5px;padding:10px 12px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px;color:#2c3e50;word-break:break-all;line-height:1.5;">%s</div>
</div>
<div style="margin-top:20px;border-left:3px solid #c9a227;padding:2px 0 2px 12px;">
<p style="margin:0;font-size:13px;line-height:1.7;color:#454b52;">不是你本人？<strong style="color:#16191d;font-weight:500;">直接忽略這封信就好。</strong>沒有點連結，登入就不會成立，你的帳號也不會有任何變動。</p>
</div>
</td></tr>
<tr><td style="padding:16px 2px 0;font-size:12px;line-height:1.6;color:#8a9199;">
系統自動寄出，請勿回覆。<br>你會收到這封信，是因為有人用這個信箱嘗試登入 ChainTrace。
</td></tr>
</table>`, safeUsername, safeURL, minutes, safeURL)

	return fmt.Sprintf("ChainTrace 登入確認（%d 分鐘內有效）", minutes), body
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
