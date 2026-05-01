package auth

import (
	"chaintrace/utils"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type JWTClaims struct {
	UserID uint   `json:"user_id"`
	IP     string `json:"ip,omitempty"`

	jwt.RegisteredClaims
}

func P2H(password string) (string, error) {
	passwordBytes := []byte(password)
	hashedPassword, err := bcrypt.GenerateFromPassword(passwordBytes, bcrypt.DefaultCost)
	return string(hashedPassword), err
}

func VP(hashedPassword string, password string) bool {
	if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password)); err != nil {
		return false
	}
	return true
}

func GenJWT(userID uint, ip string) (string, error) {
	now := time.Now()

	expireAt := now.Add(utils.TokenExpireSecond)
	claims := JWTClaims{
		UserID: userID,
		IP:     ip,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(uint64(userID), 10),
			Issuer:    "chaintrace",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expireAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString([]byte(utils.JWTSecret))
}

var (
	ErrInvalidToken = errors.New("token is invalid")
	ErrEmptyToken   = errors.New("token is empty")
)

func VerifyJWT(tokenString, ip string) (userID uint, err error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return 0, ErrEmptyToken
	}
	claims := &JWTClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// 防止 alg 被竄改，例如 none / RS256 混淆攻擊
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return []byte(utils.JWTSecret), nil
	})
	if err != nil {
		return 0, err
	}
	if token == nil || !token.Valid {
		return 0, ErrInvalidToken
	}
	if claims.Issuer != "chaintrace" {
		return 0, ErrInvalidToken
	}

	if claims.UserID == 0 {
		return 0, ErrInvalidToken
	}

	// 如果你產 token 時有寫入 IP，這裡就可以驗證
	if claims.IP != "" && ip != "" && claims.IP != ip {
		return 0, ErrInvalidToken
	}

	return claims.UserID, nil
}
