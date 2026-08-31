package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"chaintrace/analysis"
	"chaintrace/controller"
	"chaintrace/model"
	"chaintrace/model/store"
	apiRouter "chaintrace/router"

	"github.com/gin-gonic/gin"
)

// scriptedAgent is a controller.AgentProvider that answers without a sidecar,
// a model, or an API key.
type scriptedAgent struct {
	answer   string
	err      error
	requests []controller.AgentRequest
}

func (a *scriptedAgent) Respond(
	_ context.Context, request controller.AgentRequest,
) (controller.AgentResponse, error) {
	a.requests = append(a.requests, request)
	if a.err != nil {
		return controller.AgentResponse{}, a.err
	}
	return controller.AgentResponse{Content: a.answer}, nil
}

type agentSummaryResponse struct {
	DatasetID string                        `json:"datasetId"`
	Messages  []conversationMessageResponse `json:"messages"`
}

func newAgentTest(t *testing.T, agent controller.AgentProvider) (*authHTTPTest, string) {
	t.Helper()
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAgentProvider(test.engine, agent)
	return test, ownerToken(t, test.owner.ID)
}

// publishAnalysisResult writes the rows an Investigation needs to look
// analysed, without running a real Analysis Run.
func publishAnalysisResult(t *testing.T, investigationID string) string {
	t.Helper()
	datasetID := "ds-" + investigationID
	now := time.Now().UTC()
	score := 60
	dataset := store.AnalysisDataset{
		ID: datasetID, InvestigationID: investigationID, AnalysisRunID: "run-" + investigationID,
		Network: store.NetworkTRONMainnet, Asset: "USDT", CutoffBlockID: "0000000000012345",
		WindowStart: now.Add(-30 * 24 * time.Hour), WindowEnd: now,
		TransferLimit: 500, TraversalDepth: 2, CollectedTransfers: 1, ReachedDepth: 1,
		Partial: false, Confidence: 90, StopReason: analysis.StopReasonSourceExhausted,
		CreatedAt: now,
	}
	if err := model.DB.Create(&dataset).Error; err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	metrics := store.InvestigationMetrics{
		DatasetID: datasetID, RelatedNodes: 2, TransferCount: 1,
		TotalFlowSmallestUnit: "100000000", TotalFlowDecimals: 6, Asset: "USDT", CreatedAt: now,
	}
	if err := model.DB.Create(&metrics).Error; err != nil {
		t.Fatalf("create metrics: %v", err)
	}
	assessment := store.Assessment{
		DatasetID: datasetID, Score: &score, Level: "high",
		ReasonsJSON: `["fan_out"]`, NodeAssessmentsJSON: `[]`,
		Source: "rules-v1", UpdatedAt: now,
	}
	if err := model.DB.Create(&assessment).Error; err != nil {
		t.Fatalf("create assessment: %v", err)
	}
	if err := model.DB.Model(&store.Investigation{}).Where("id = ?", investigationID).
		Update("current_result_id", datasetID).Error; err != nil {
		t.Fatalf("attach current result: %v", err)
	}
	return datasetID
}

func TestConversationStoresTheAgentAnswer(t *testing.T) {
	agent := &scriptedAgent{answer: "這個地址轉出給 14 個不同地址。"}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "With agent")

	response := test.request(http.MethodPost,
		"/api/v1/investigations/"+investigationID+"/conversation",
		map[string]string{"idempotencyKey": "cmd-1", "message": "有什麼異常?"}, token)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var got struct {
		Messages []conversationMessageResponse `json:"messages"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %#v, want the question and the answer", got.Messages)
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content != "有什麼異常?" {
		t.Errorf("question = %#v", got.Messages[0])
	}
	if got.Messages[1].Role != "agent" || got.Messages[1].Content != agent.answer {
		t.Errorf("answer = %#v", got.Messages[1])
	}
}

// The provider must never be handed an Owner id from the request body.
func TestConversationPassesTheAuthenticatedOwner(t *testing.T) {
	agent := &scriptedAgent{answer: "ok"}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "Owner check")

	test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/conversation",
		map[string]string{"idempotencyKey": "cmd-1", "message": "hi"}, token)

	if len(agent.requests) != 1 {
		t.Fatalf("provider calls = %d", len(agent.requests))
	}
	request := agent.requests[0]
	if request.OwnerID != test.owner.ID || request.InvestigationID != investigationID {
		t.Errorf("request identity = %+v", request)
	}
	if request.Mode != controller.AgentModeChat {
		t.Errorf("mode = %q, want chat", request.Mode)
	}
	// The unsent question must be part of the context the Agent reasons over.
	last := request.Conversation[len(request.Conversation)-1]
	if last.Role != store.ConversationRoleUser || last.Content != "hi" {
		t.Errorf("conversation tail = %+v", last)
	}
}

func TestConversationCarriesEarlierTurnsToTheAgent(t *testing.T) {
	agent := &scriptedAgent{answer: "第二個答案"}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "History")
	path := "/api/v1/investigations/" + investigationID + "/conversation"

	test.request(http.MethodPost, path,
		map[string]string{"idempotencyKey": "cmd-1", "message": "第一個問題"}, token)
	test.request(http.MethodPost, path,
		map[string]string{"idempotencyKey": "cmd-2", "message": "第二個問題"}, token)

	if len(agent.requests) != 2 {
		t.Fatalf("provider calls = %d", len(agent.requests))
	}
	second := agent.requests[1].Conversation
	if len(second) != 3 {
		t.Fatalf("second turn context = %#v, want question, answer, question", second)
	}
	if second[0].Content != "第一個問題" || second[1].Role != store.ConversationRoleAgent {
		t.Errorf("history = %#v", second)
	}
}

// A failing Agent degrades; it never invents an answer.
func TestConversationDegradesWhenTheAgentFails(t *testing.T) {
	agent := &scriptedAgent{err: context.DeadlineExceeded}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "Failing agent")

	response := test.request(http.MethodPost,
		"/api/v1/investigations/"+investigationID+"/conversation",
		map[string]string{"idempotencyKey": "cmd-1", "message": "有什麼異常?"}, token)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body: %s",
			response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	var got struct {
		Code      string                        `json:"code"`
		Persisted bool                          `json:"persisted"`
		Messages  []conversationMessageResponse `json:"messages"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Code != "agent_unavailable" || !got.Persisted {
		t.Errorf("response = %#v", got)
	}
	// The Owner's question survives even though the Agent did not answer.
	if len(got.Messages) != 2 || got.Messages[0].Content != "有什麼異常?" {
		t.Errorf("messages = %#v", got.Messages)
	}
}

// A brand new Investigation is conversable: the Agent can be asked to start
// one (ADR-0013), so refusing to talk before an analysis exists would make the
// whole opening move impossible.
func TestConversationWorksBeforeAnyAnalysis(t *testing.T) {
	agent := &scriptedAgent{answer: "給我一個地址，我就開始查。"}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "No analysis")
	path := "/api/v1/investigations/" + investigationID + "/conversation"

	response := test.request(http.MethodPost, path,
		map[string]string{"idempotencyKey": "cmd-1", "message": "你能做什麼?"}, token)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s",
			response.Code, http.StatusOK, response.Body.String())
	}
	var got struct {
		Messages []conversationMessageResponse `json:"messages"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Messages) != 2 || got.Messages[1].Content != agent.answer {
		t.Errorf("messages = %#v", got.Messages)
	}
}

func TestSummaryIsStoredAsAnAgentMessage(t *testing.T) {
	agent := &scriptedAgent{answer: "### 範圍聲明\n本次分析涵蓋 30 天。"}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "Summary")
	datasetID := publishAnalysisResult(t, investigationID)

	response := test.request(http.MethodPost,
		"/api/v1/investigations/"+investigationID+"/agent/summary", nil, token)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s",
			response.Code, http.StatusCreated, response.Body.String())
	}
	var got agentSummaryResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.DatasetID != datasetID {
		t.Errorf("datasetId = %q, want %q", got.DatasetID, datasetID)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "agent" {
		t.Fatalf("messages = %#v, want one agent message", got.Messages)
	}
	if agent.requests[0].Mode != controller.AgentModeSummary {
		t.Errorf("mode = %q, want summary", agent.requests[0].Mode)
	}
}

// Regenerating a summary for the same dataset must not spend tokens again.
func TestSummaryIsIdempotentPerDataset(t *testing.T) {
	agent := &scriptedAgent{answer: "第一版摘要"}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "Idempotent summary")
	publishAnalysisResult(t, investigationID)
	path := "/api/v1/investigations/" + investigationID + "/agent/summary"

	first := test.request(http.MethodPost, path, nil, token)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d; body: %s", first.Code, first.Body.String())
	}
	agent.answer = "第二版摘要"
	second := test.request(http.MethodPost, path, nil, token)

	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d; body: %s",
			second.Code, http.StatusOK, second.Body.String())
	}
	var got agentSummaryResponse
	if err := json.Unmarshal(second.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "第一版摘要" {
		t.Errorf("messages = %#v, want the stored first summary", got.Messages)
	}
	if len(agent.requests) != 1 {
		t.Errorf("provider calls = %d, want 1", len(agent.requests))
	}
}

func TestSummaryWithoutAnalysisIsNotFound(t *testing.T) {
	agent := &scriptedAgent{answer: "unused"}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "No result")

	response := test.request(http.MethodPost,
		"/api/v1/investigations/"+investigationID+"/agent/summary", nil, token)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s",
			response.Code, http.StatusNotFound, response.Body.String())
	}
	if len(agent.requests) != 0 {
		t.Error("the Agent was called even though there was nothing to summarise")
	}
}

func TestSummaryWithoutAnAgentReportsUnavailable(t *testing.T) {
	test, token := newAgentTest(t, nil)
	investigationID := createConversationInvestigation(t, test, token, "No agent")
	publishAnalysisResult(t, investigationID)

	response := test.request(http.MethodPost,
		"/api/v1/investigations/"+investigationID+"/agent/summary", nil, token)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body: %s",
			response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}

func TestAnotherOwnerCannotReachTheAgent(t *testing.T) {
	agent := &scriptedAgent{answer: "leak"}
	test, token := newAgentTest(t, agent)
	investigationID := createConversationInvestigation(t, test, token, "Owner scoped")
	publishAnalysisResult(t, investigationID)

	intruder := store.User{
		Username: "intruder", DisplayName: "Intruder",
		Email: "intruder@auth-test.invalid", Password: "x", Salt: "y",
	}
	if err := model.DB.Create(&intruder).Error; err != nil {
		t.Fatalf("create intruder: %v", err)
	}
	intruderToken := ownerToken(t, intruder.ID)

	summary := test.request(http.MethodPost,
		"/api/v1/investigations/"+investigationID+"/agent/summary", nil, intruderToken)
	if summary.Code != http.StatusNotFound {
		t.Errorf("summary status = %d, want %d; body: %s",
			summary.Code, http.StatusNotFound, summary.Body.String())
	}
	if len(agent.requests) != 0 {
		t.Error("the Agent was reached by someone who does not own the Investigation")
	}
}
