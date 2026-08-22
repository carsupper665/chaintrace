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
	if userID == 0 || strings.TrimSpace(utils.JWTSecret) == "" {
		return "", ErrInvalidToken
	}
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
	if strings.TrimSpace(utils.JWTSecret) == "" {
		return 0, ErrInvalidToken
	}
	claims := &JWTClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return []byte(utils.JWTSecret), nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer("chaintrace"),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return 0, err
	}
	if token == nil || !token.Valid {
		return 0, ErrInvalidToken
	}
	if claims.Issuer != "chaintrace" || claims.IssuedAt == nil || claims.NotBefore == nil || claims.ExpiresAt == nil {
		return 0, ErrInvalidToken
	}

	if claims.UserID == 0 || claims.Subject != strconv.FormatUint(uint64(claims.UserID), 10) {
		return 0, ErrInvalidToken
	}

	// claims.IP is kept as an issuance audit trail but deliberately not compared:
	// every request reaches this API from the frontend BFF, so it records the BFF's
	// address rather than the Owner's and would only reject valid credentials.

	return claims.UserID, nil
}
