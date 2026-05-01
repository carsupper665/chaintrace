package router

import (
	"chaintrace/controller"

	"github.com/gin-gonic/gin"
)

func ApiRouter(router *gin.Engine) {
	lc := controller.NewChallengeStore()
	api := router.Group("/api")
	v1 := api.Group("/v1")

	{
		v1.POST("/register", controller.RegisterNewUser)
	}
	auth := router.Group("/Authentication")
	{
		auth.POST("/login", lc.ChallengeLogin)
		auth.GET("/verify", lc.UrlVerifyLogin)
		auth.GET("/challenge", lc.ExchangeToken)
	}
}
