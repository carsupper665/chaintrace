package controller

import (
	"errors"
	"net/http"

	"chaintrace/analysis"
	"chaintrace/model"
	"chaintrace/model/store"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// summaryKeyPrefix scopes a summary's idempotency key to the dataset it
// describes. A new Analysis Run produces a new dataset id and therefore a new
// key, so each set of evidence gets exactly one summary.
const summaryKeyPrefix = "summary:"

// summaryInstruction is the Owner turn that opens a summary. It is not stored;
// the summary itself is what enters the conversation.
const summaryInstruction = "請為這份調查產生摘要。"

type AgentHandler struct {
	provider AgentProvider
}

func NewAgentHandler(provider AgentProvider) *AgentHandler {
	return &AgentHandler{provider: provider}
}

// GenerateSummary produces the opening summary for an Investigation's current
// analysis result and stores it as an Agent message in the conversation.
//
// Repeating the request for the same dataset returns the stored summary rather
// than generating a second one.
func (h *AgentHandler) GenerateSummary(c *gin.Context) {
	ownerID := c.GetUint("user_id")
	investigationID := c.Param("id")

	if h.provider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "agent_unavailable", "message": "Agent is not configured",
		})
		return
	}

	current, err := analysis.LoadCurrentResult(ownerID, investigationID)
	if err != nil {
		if errors.Is(err, analysis.ErrCurrentResultNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"code": "current_result_not_found", "message": "尚未有可用的分析結果",
			})
			return
		}
		writeInvestigationLookupError(c, err)
		return
	}

	key := summaryKeyPrefix + current.Dataset.ID
	existing, err := model.FindConversationGroup(ownerID, investigationID, key)
	if err != nil {
		writeInvestigationLookupError(c, err)
		return
	}
	if len(existing) > 0 {
		c.JSON(http.StatusOK, gin.H{
			"datasetId": current.Dataset.ID, "messages": conversationDTOs(existing),
		})
		return
	}

	answer, err := h.provider.Respond(c.Request.Context(), AgentRequest{
		OwnerID:         ownerID,
		InvestigationID: investigationID,
		Mode:            AgentModeSummary,
		Conversation: []AgentConversationMessage{
			{Role: store.ConversationRoleUser, Content: summaryInstruction},
		},
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeInvestigationLookupError(c, err)
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "agent_unavailable", "message": "Agent did not produce a summary",
		})
		return
	}

	messages, err := model.PersistAgentMessage(ownerID, investigationID, key, answer.Content)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeInvestigationLookupError(c, err)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": "internal_error", "message": "Internal server error",
		})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"datasetId": current.Dataset.ID, "messages": conversationDTOs(messages),
	})
}
