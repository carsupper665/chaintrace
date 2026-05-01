package router

import (
	"chaintrace/middleware"
	"chaintrace/utils"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func SetRouter(router *gin.Engine) {

	maxReqNum := utils.GetEnvInt("GLOBAL_MAX_REQUEST_NUM", 100)

	router.Use(middleware.CORS(), middleware.IpRateLimiter(maxReqNum, 60))
	ApiRouter(router)

	frontendBaseUrl := utils.FrontEndUrl

	frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
	router.NoRoute(func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
	})

}
