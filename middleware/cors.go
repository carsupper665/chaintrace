package middleware

import (
	"chaintrace/utils"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowOrigins = append([]string(nil), utils.AllowedOrigins...)
	config.AllowCredentials = true
	config.AllowMethods = []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"Authorization", "Content-Type", "X-Request-Id"}
	return cors.New(config)
}
