package router

import (
	"context"
	"errors"

	"chaintrace/agentclient"
	"chaintrace/analysis"
	"chaintrace/claimclient"
	"chaintrace/controller"
	"chaintrace/middleware"
	"chaintrace/scoreclient"
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
		ProductionLimits(), productionAgentProvider, productionClaimExtractor(),
	)
}

func ApiRouterWithAuthOptions(router *gin.Engine, options controller.AuthOptions) {
	apiRouterWithOptions(
		router, options, productionAnalysisOptions(), Limits{}, nil, productionClaimExtractor(),
	)
}

// ApiRouterWithAgentProvider injects an Agent, which is how the HTTP tests
// exercise the conversation and summary paths without a sidecar.
func ApiRouterWithAgentProvider(router *gin.Engine, provider controller.AgentProvider) {
	apiRouterWithOptions(
		router, controller.AuthOptions{}, productionAnalysisOptions(), Limits{},
		func(*analysis.RunManager) controller.AgentProvider { return provider },
		productionClaimExtractor(),
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
	return analysis.Options{Provider: provider, LearnedScorer: productionLearnedScorer()}
}

// productionLearnedScorer returns a nil interface — not a nil *scoreclient.Client
// — when no scorer is configured, the same care as productionAgentProvider.
// It is the same sidecar, so it reuses the Agent's own two variables rather
// than inventing more. A missing scorer is not worth a startup warning: every
// run simply completes rules-only.
func productionLearnedScorer() analysis.LearnedScorer {
	client := scoreclient.New(
		utils.GetEnvString("AGENT_BASE_URL", ""),
		utils.GetEnvString("AGENT_SHARED_KEY", ""),
	)
	if client == nil {
		return nil
	}
	return client
}

// productionClaimExtractor returns a nil interface when the sidecar is not
// configured, matching productionAgentProvider / productionLearnedScorer.
// claimclient stays an HTTP adapter; the controller seam uses neutral DTOs.
func productionClaimExtractor() controller.ClaimExtractor {
	client := claimclient.New(
		utils.GetEnvString("AGENT_BASE_URL", ""),
		utils.GetEnvString("AGENT_SHARED_KEY", ""),
	)
	if client == nil {
		return nil
	}
	return claimClientAdapter{client: client}
}

type claimClientAdapter struct {
	client *claimclient.Client
}

func (a claimClientAdapter) ExtractPNG(
	ctx context.Context, image []byte,
) (controller.ClaimExtractResult, error) {
	result, err := a.client.ExtractPNG(ctx, image)
	if err != nil {
		var extractErr *claimclient.ExtractError
		if errors.As(err, &extractErr) {
			return controller.ClaimExtractResult{}, &controller.ClaimExtractError{
				Code: extractErr.Code, Message: extractErr.Message, Status: extractErr.Status,
			}
		}
		return controller.ClaimExtractResult{}, err
	}
	claims := make([]controller.AddressClaim, len(result.Claims))
	for index, claim := range result.Claims {
		claims[index] = controller.AddressClaim{
			Kind: claim.Kind, Value: claim.Value, Role: claim.Role, Confidence: claim.Confidence,
		}
	}
	return controller.ClaimExtractResult{Network: result.Network, Claims: claims}, nil
}

func ProductionLimits() Limits {
	return Limits{
		UnauthenticatedRequestsPerMinute: utils.GetEnvInt("GLOBAL_MAX_REQUEST_NUM", 100),
		OwnerRequestsPerMinute:           utils.GetEnvInt("OWNER_MAX_REQUEST_NUM", 600),
	}
}

func ApiRouterWithAnalysisOptions(router *gin.Engine, options analysis.Options) {
	apiRouterWithOptions(router, controller.AuthOptions{}, options, Limits{}, nil, productionClaimExtractor())
}

// ApiRouterWithClaimOptions injects extract + chain-provider doubles for the
// screenshot Claim HTTP journey without requiring a live Agent sidecar.
func ApiRouterWithClaimOptions(
	router *gin.Engine,
	options analysis.Options,
	extractor controller.ClaimExtractor,
) {
	apiRouterWithOptions(router, controller.AuthOptions{}, options, Limits{}, nil, extractor)
}

func apiRouterWithOptions(
	router *gin.Engine,
	authOptions controller.AuthOptions,
	analysisOptions analysis.Options,
	limits Limits,
	newAgent agentFactory,
	claimExtractor controller.ClaimExtractor,
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
	claimHandler := controller.NewClaimHandler(claimExtractor, analysisOptions.Provider)
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
		protected.POST("/investigations/:id/conversation/stream", conversationHandler.SubmitConversationStream)
		protected.POST("/investigations/:id/agent/summary", agentHandler.GenerateSummary)
		protected.GET("/agent/models", agentHandler.ListModels)
		protected.POST("/claims/extract", claimHandler.Extract)
		protected.POST("/claims/verify-address", claimHandler.VerifyAddress)
	}
	auth := router.Group("/Authentication", unauthenticated)
	{
		auth.POST("/login", lc.ChallengeLogin)
		auth.GET("/verify", lc.UrlVerifyLogin)
		auth.GET("/challenge", lc.ExchangeToken)
		auth.GET("/fgf/login", controller.FGFLogin)
		auth.POST("/fgf/callback", controller.FGFCallback)
	}
}
