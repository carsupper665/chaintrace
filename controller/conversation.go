package controller

import (
	"chaintrace/model"
	"chaintrace/model/store"
	"chaintrace/utils"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	defaultConversationPageSize = 50
	maximumConversationPageSize = 100
	maximumConversationBodySize = 1 << 20
	maximumConversationMessage  = 256 << 10
	maximumIdempotencyKeySize   = 128
	// agentHistoryMessages caps how much transcript the Agent sees. The tail is
	// what matters for context, and an unbounded history would eventually
	// exceed the model's input budget.
	agentHistoryMessages = 40
)

type AgentProvider interface {
	Respond(context.Context, AgentRequest) (AgentResponse, error)
}

// AgentMode selects which prompt the Agent runs. Summary opens an
// Investigation with an overview; chat answers an Owner's question.
type AgentMode string

const (
	AgentModeSummary AgentMode = "summary"
	AgentModeChat    AgentMode = "chat"
)

// AgentRequest carries only what the provider cannot derive itself. The
// evidence pack is assembled by the provider from the database, so passing a
// dataset summary here would only create a second place for it to drift.
type AgentRequest struct {
	OwnerID         uint
	InvestigationID string
	Mode            AgentMode
	Conversation    []AgentConversationMessage
}

type AgentConversationMessage struct {
	Role    store.ConversationRole
	Content string
}

type AgentResponse struct {
	Content string
}

type ConversationHandler struct {
	provider AgentProvider
}

type submitConversationRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	Message        string `json:"message"`
}

type conversationMessageDTO struct {
	ID        string                 `json:"id"`
	Role      store.ConversationRole `json:"role"`
	Content   string                 `json:"content"`
	CreatedAt time.Time              `json:"createdAt"`
}

func NewConversationHandler(provider AgentProvider) *ConversationHandler {
	return &ConversationHandler{provider: provider}
}

func (h *ConversationHandler) GetConversation(c *gin.Context) {
	pageSize, ok := conversationPageSize(c)
	if !ok {
		writeInvalidConversationQuery(c)
		return
	}
	cursor := ""
	if values, exists := c.Request.URL.Query()["cursor"]; exists {
		if len(values) != 1 || values[0] == "" {
			writeInvalidConversationCursor(c)
			return
		}
		cursor = values[0]
	}
	page, err := model.LoadConversationPage(c.GetUint("user_id"), c.Param("id"), cursor, pageSize)
	if err != nil {
		if errors.Is(err, model.ErrInvalidConversationCursor) {
			writeInvalidConversationCursor(c)
			return
		}
		writeInvestigationLookupError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": conversationDTOs(page.Messages), "nextCursor": page.NextCursor})
}

// decodeSubmitRequest reads and validates the request body, answering the
// client itself when anything is wrong.
func decodeSubmitRequest(c *gin.Context) (submitConversationRequest, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maximumConversationBodySize)
	var request submitConversationRequest
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&request); err != nil {
		writeInvalidConversationRequest(c)
		return request, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeInvalidConversationRequest(c)
		return request, false
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if !validBoundedText(request.IdempotencyKey, maximumIdempotencyKeySize) ||
		!validBoundedText(request.Message, maximumConversationMessage) {
		writeInvalidConversationRequest(c)
		return request, false
	}
	return request, true
}

// validBoundedText accepts non-blank, valid UTF-8 within a byte budget.
func validBoundedText(value string, maximumBytes int) bool {
	return strings.TrimSpace(value) != "" &&
		len(value) <= maximumBytes &&
		utf8.ValidString(value)
}

func (h *ConversationHandler) SubmitConversation(c *gin.Context) {
	request, ok := decodeSubmitRequest(c)
	if !ok {
		return
	}
	ownerID := c.GetUint("user_id")
	investigationID := c.Param("id")
	if h.provider == nil {
		h.writeUnavailable(c, ownerID, investigationID, request, nil)
		return
	}

	history, err := model.LoadRecentConversation(ownerID, investigationID, agentHistoryMessages)
	if err != nil {
		writeInvestigationLookupError(c, err)
		return
	}
	// The Owner's new message is not stored yet, so it is appended here rather
	// than read back; storing it first would leave a dangling question if the
	// Agent then failed.
	conversation := append(agentConversation(history), AgentConversationMessage{
		Role: store.ConversationRoleUser, Content: request.Message,
	})

	answer, err := h.provider.Respond(c.Request.Context(), AgentRequest{
		OwnerID:         ownerID,
		InvestigationID: investigationID,
		Mode:            AgentModeChat,
		Conversation:    conversation,
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeInvestigationLookupError(c, err)
			return
		}
		h.writeUnavailable(c, ownerID, investigationID, request, err)
		return
	}

	messages, err := model.PersistConversationTurn(
		ownerID, investigationID, request.IdempotencyKey, request.Message, answer.Content,
	)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeInvestigationLookupError(c, err)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": conversationDTOs(messages)})
}

// writeUnavailable stores the Owner's message with a machine outcome and
// reports the Agent as unavailable. The question is never silently dropped.
//
// cause is logged rather than returned: the Owner gets a stable message, but a
// degraded turn with no recorded reason is impossible to diagnose afterwards.
func (h *ConversationHandler) writeUnavailable(
	c *gin.Context, ownerID uint, investigationID string,
	request submitConversationRequest, cause error,
) {
	if cause != nil && utils.SysLog != nil {
		utils.SysLog.Errorf(
			"Agent turn failed for investigation %s: %v, ReqId: %s",
			investigationID, cause, c.GetString(utils.RequestIdKey),
		)
	}
	messages, err := model.PersistUnavailableConversation(
		ownerID, investigationID, request.IdempotencyKey, request.Message,
	)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeInvestigationLookupError(c, err)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"code": "agent_unavailable", "persisted": true, "messages": conversationDTOs(messages),
	})
}

func agentConversation(messages []model.ConversationMessage) []AgentConversationMessage {
	result := make([]AgentConversationMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, AgentConversationMessage{Role: message.Role, Content: message.Content})
	}
	return result
}

func conversationPageSize(c *gin.Context) (int, bool) {
	values, exists := c.Request.URL.Query()["pageSize"]
	if !exists {
		return defaultConversationPageSize, true
	}
	if len(values) != 1 {
		return 0, false
	}
	pageSize, err := strconv.Atoi(values[0])
	return pageSize, err == nil && pageSize >= 1 && pageSize <= maximumConversationPageSize
}

func conversationDTOs(messages []model.ConversationMessage) []conversationMessageDTO {
	result := make([]conversationMessageDTO, 0, len(messages))
	for _, message := range messages {
		result = append(result, conversationMessageDTO{
			ID: message.ID, Role: message.Role, Content: message.Content, CreatedAt: message.CreatedAt,
		})
	}
	return result
}

func writeInvalidConversationRequest(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "validation_error", "message": "Invalid request"})
}

func writeInvalidConversationQuery(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_conversation_query", "message": "Invalid conversation query"})
}

func writeInvalidConversationCursor(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_conversation_cursor", "message": "Invalid conversation cursor"})
}
