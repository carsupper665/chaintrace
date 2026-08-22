package router

import (
	"chaintrace/analysis"
	"chaintrace/controller"
	"chaintrace/middleware"
	"chaintrace/utils"

	"github.com/gin-gonic/gin"
)

// Limits are request budgets per 60-second window. A non-positive budget
// disables that limiter, which is how the HTTP tests opt out.
type Limits struct {
	// UnauthenticatedRequestsPerMinute is keyed by client IP.
	UnauthenticatedRequestsPerMinute int
	// OwnerRequestsPerMinute is keyed by the authenticated Owner. Protected
	// traffic cannot be keyed by IP because it all arrives from the BFF.
	OwnerRequestsPerMinute int
}

func ApiRouter(router *gin.Engine) {
	apiRouterWithOptions(router, controller.AuthOptions{}, productionAnalysisOptions(), ProductionLimits())
}

func ApiRouterWithAuthOptions(router *gin.Engine, options controller.AuthOptions) {
	apiRouterWithOptions(router, options, productionAnalysisOptions(), Limits{})
}

func productionAnalysisOptions() analysis.Options {
	provider := analysis.ProductionTronGridProvider()
	if provider == nil && utils.SysLog != nil {
		utils.SysLog.Warnf(
			"TRONGRID_API_KEY is not configured: no chain data provider is available and every Analysis Run will fail with provider_unavailable",
		)
	}
	return analysis.Options{Provider: provider}
}

func ProductionLimits() Limits {
	return Limits{
		UnauthenticatedRequestsPerMinute: utils.GetEnvInt("GLOBAL_MAX_REQUEST_NUM", 100),
		OwnerRequestsPerMinute:           utils.GetEnvInt("OWNER_MAX_REQUEST_NUM", 600),
	}
}

func ApiRouterWithAnalysisOptions(router *gin.Engine, options analysis.Options) {
	apiRouterWithOptions(router, controller.AuthOptions{}, options, Limits{})
}

func apiRouterWithOptions(router *gin.Engine, authOptions controller.AuthOptions, analysisOptions analysis.Options, limits Limits) {
	lc := controller.NewChallengeStore(authOptions)
	runManager := analysis.NewRunManager(analysisOptions)
	analysisHandler := controller.NewAnalysisHandler(runManager)
	investigationHandler := controller.NewInvestigationHandler(runManager)
	conversationHandler := controller.NewConversationHandler(nil)
	// One bucket shared by every unauthenticated endpoint, per client IP.
	unauthenticated := middleware.IpRateLimiter(limits.UnauthenticatedRequestsPerMinute, 60)
	api := router.Group("/api")
	v1 := api.Group("/v1")

	{
		v1.POST("/register", unauthenticated, controller.RegisterNewUser)
	}
	protected := v1.Group("")
	protected.Use(
		middleware.ValidateJWT(),
		middleware.OwnerRateLimiter(limits.OwnerRequestsPerMinute, 60),
	)
	{
		protected.GET("/me", controller.CurrentUser)
		protected.POST("/logout", controller.Logout)
		protected.GET("/investigations", controller.ListInvestigations)
		protected.POST("/investigations", controller.CreateInvestigation)
		protected.GET("/investigations/:id", controller.GetInvestigation)
		protected.PATCH("/investigations/:id", controller.UpdateInvestigation)
		protected.DELETE("/investigations/:id", investigationHandler.DeleteInvestigation)
		protected.POST("/investigations/:id/analysis-runs", analysisHandler.StartRun)
		protected.GET("/investigations/:id/analysis-runs/:runId", analysisHandler.GetRun)
		protected.DELETE("/investigations/:id/analysis-runs/:runId", analysisHandler.CancelRun)
		protected.GET("/investigations/:id/current-result", analysisHandler.GetCurrentResult)
		protected.GET("/investigations/:id/graph", analysisHandler.GetGraph)
		protected.GET("/investigations/:id/conversation", conversationHandler.GetConversation)
		protected.POST("/investigations/:id/conversation", conversationHandler.SubmitConversation)
	}
	auth := router.Group("/Authentication", unauthenticated)
	{
		auth.POST("/login", lc.ChallengeLogin)
		auth.GET("/verify", lc.UrlVerifyLogin)
		auth.GET("/challenge", lc.ExchangeToken)
	}
}
