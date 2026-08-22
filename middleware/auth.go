package middleware

import (
	"chaintrace/auth"
	"chaintrace/model"
	"chaintrace/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func ValidateJWT() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.Request.Header.Get("Authorization")
		parts := strings.Fields(authHeader)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
			abortUnauthorized(c)
			return
		}
		token := parts[1]

		if auth.RTS == nil || auth.RTS.IsRevoked(token) {
			abortUnauthorized(c)
			return
		}

		ip := c.ClientIP()
		userID, err := auth.VerifyJWT(token, ip)
		if err != nil {
			if utils.SysLog != nil {
				utils.SysLog.Errorf("Token verification error: %v, ReqId: %s", err, c.GetString(utils.RequestIdKey))
			}
			abortUnauthorized(c)
			return
		}
		if _, err := model.GetUserByID(userID); err != nil {
			abortUnauthorized(c)
			return
		}
		c.Set("user_id", userID)
		c.Set("auth_token", token)
		c.Next()
	}
}

func abortUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "unauthorized", "message": "Unauthorized"})
}
