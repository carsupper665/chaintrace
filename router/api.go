package router

import (
	"chaintrace/agentclient"
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

// agentFactory builds the Agent once the RunManager it needs exists. The Agent
// can start Analysis Runs, so it has to be constructed after the manager that
// owns them.
type agentFactory func(*analysis.RunManager) controller.AgentProvider

func ApiRouter(router *gin.Engine) {
	apiRouterWithOptions(
		router, controller.AuthOptions{}, productionAnalysisOptions(),
		ProductionLimits(), productionAgentProvider,
	)
}

func ApiRouterWithAuthOptions(router *gin.Engine, options controller.AuthOptions) {
	apiRouterWithOptions(router, options, productionAnalysisOptions(), Limits{}, nil)
}

// ApiRouterWithAgentProvider injects an Agent, which is how the HTTP tests
// exercise the conversation and summary paths without a sidecar.
func ApiRouterWithAgentProvider(router *gin.Engine, provider controller.AgentProvider) {
	apiRouterWithOptions(
		router, controller.AuthOptions{}, productionAnalysisOptions(), Limits{},
		func(*analysis.RunManager) controller.AgentProvider { return provider },
	)
}

// productionAgentProvider returns a nil interface — not a nil *Provider — when
// no Agent is configured. A typed nil in an interface is non-nil, which would
// turn "no Agent" into a panic on the first conversation request.
func productionAgentProvider(runs *analysis.RunManager) controller.AgentProvider {
	options := agentclient.ProductionOptions()
	provider := agentclient.New(options, runs)
	if provider == nil {
		if warning := options.ConfigurationWarning(); warning != "" && utils.SysLog != nil {
			utils.SysLog.Warnf(
				"%s: the Agent stays disabled and every conversation request answers agent_unavailable without contacting the sidecar",
				warning,
			)
		}
		return nil
	}
	if utils.SysLog != nil {
		utils.SysLog.Infof("Agent sidecar enabled at %s", options.BaseURL)
	}
	return provider
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
	apiRouterWithOptions(router, controller.AuthOptions{}, options, Limits{}, nil)
}

func apiRouterWithOptions(
	router *gin.Engine,
	authOptions controller.AuthOptions,
	analysisOptions analysis.Options,
	limits Limits,
	newAgent agentFactory,
) {
	lc := controller.NewChallengeStore(authOptions)
	runManager := analysis.NewRunManager(analysisOptions)
	var agentProvider controller.AgentProvider
	if newAgent != nil {
		agentProvider = newAgent(runManager)
	}
	analysisHandler := controller.NewAnalysisHandler(runManager)
	investigationHandler := controller.NewInvestigationHandler(runManager)
	conversationHandler := controller.NewConversationHandler(agentProvider)
	agentHandler := controller.NewAgentHandler(agentProvider)
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
		protected.POST("/investigations/:id/agent/summary", agentHandler.GenerateSummary)
	}
	auth := router.Group("/Authentication", unauthenticated)
	{
		auth.POST("/login", lc.ChallengeLogin)
		auth.GET("/verify", lc.UrlVerifyLogin)
		auth.GET("/challenge", lc.ExchangeToken)
	}
}
