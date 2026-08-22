package controller

import (
	"chaintrace/model"
	"chaintrace/model/store"
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
)

type AgentProvider interface {
	Respond(context.Context, AgentRequest) (AgentResponse, error)
}

type AgentRequest struct {
	InvestigationID string
	TargetAddress   *string
	Network         store.InvestigationNetwork
	Dataset         *AgentDatasetContext
	Conversation    []AgentConversationMessage
}

type AgentDatasetContext struct {
	ID         string
	Partial    bool
	Confidence int
	StopReason string
	RiskScore  *int
	RiskLevel  string
	Reasons    []string
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

func (h *ConversationHandler) SubmitConversation(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maximumConversationBodySize)
	var request submitConversationRequest
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&request); err != nil {
		writeInvalidConversationRequest(c)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeInvalidConversationRequest(c)
		return
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.IdempotencyKey == "" || len(request.IdempotencyKey) > maximumIdempotencyKeySize ||
		!utf8.ValidString(request.IdempotencyKey) || strings.TrimSpace(request.Message) == "" ||
		len(request.Message) > maximumConversationMessage || !utf8.ValidString(request.Message) {
		writeInvalidConversationRequest(c)
		return
	}
	if h.provider != nil {
		c.JSON(http.StatusNotImplemented, gin.H{"code": "agent_not_implemented", "message": "Agent provider execution is not implemented"})
		return
	}
	messages, err := model.PersistUnavailableConversation(c.GetUint("user_id"), c.Param("id"), request.IdempotencyKey, request.Message)
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
