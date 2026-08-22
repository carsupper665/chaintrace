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

	router.Use(middleware.CORS())
	// Rate limiting lives inside ApiRouter, where the authentication boundary is
	// known: unauthenticated routes are budgeted per IP, protected routes per
	// Owner. See ProductionLimits.
	ApiRouter(router)

	frontendBaseUrl := utils.FrontEndUrl

	frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
	router.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "Not found"})
			return
		}
		c.Redirect(http.StatusFound, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
	})

}
