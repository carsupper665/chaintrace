package middleware

import (
	"chaintrace/auth"
	"chaintrace/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func ValidateJWT() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.Request.Header.Get("Authorization")

		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Authorization header is empty"})
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")

		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Token is required"})
			return
		}

		if ok := auth.RTS.IsRevoked(token); !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Token is revoked"})
			return
		}

		ip := c.ClientIP()
		userID, err := auth.VerifyJWT(token, ip)
		if err != nil {
			utils.SysLog.Errorf("Token verification error: %v, ReqId: %s", err, c.Request.Header.Get("X-Request-Id"))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Token is invalid"})
			return
		}
		c.Set("user_id", userID)
		c.Next()
	}
}
