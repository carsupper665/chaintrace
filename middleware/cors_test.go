package middleware_test

import (
	"chaintrace/middleware"
	"chaintrace/utils"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCredentialedCORSUsesConfiguredOrigin(t *testing.T) {
	previous := utils.AllowedOrigins
	t.Cleanup(func() { utils.AllowedOrigins = previous })
	utils.AllowedOrigins = []string{"https://frontend.test"}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.CORS())
	engine.GET("/api/v1/me", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	allowed := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	allowed.Header.Set("Origin", "https://frontend.test")
	allowedResponse := httptest.NewRecorder()
	engine.ServeHTTP(allowedResponse, allowed)
	if got := allowedResponse.Header().Get("Access-Control-Allow-Origin"); got != "https://frontend.test" {
		t.Errorf("allowed origin header = %q, want configured origin", got)
	}
	if got := allowedResponse.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("credentials header = %q, want true", got)
	}

	disallowed := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	disallowed.Header.Set("Origin", "https://attacker.test")
	disallowedResponse := httptest.NewRecorder()
	engine.ServeHTTP(disallowedResponse, disallowed)
	if got := disallowedResponse.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin header = %q, want empty", got)
	}
}
