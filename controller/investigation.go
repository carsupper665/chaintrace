package controller

import (
	"chaintrace/analysis"
	"chaintrace/model"
	"chaintrace/model/store"
	"chaintrace/utils"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type investigationRequest struct {
	Title   string  `json:"title"`
	Address *string `json:"address"`
}

type updateInvestigationRequest struct {
	Title   *string `json:"title"`
	Address *string `json:"address"`
}

const maxInvestigationTitleLength = 120

var lastInvestigationCreatedMillisecond atomic.Int64

type investigationDTO struct {
	ID               string                     `json:"id"`
	Title            string                     `json:"title"`
	Address          *string                    `json:"address"`
	Network          store.InvestigationNetwork `json:"network"`
	Status           store.InvestigationStatus  `json:"status"`
	TargetLocked     bool                       `json:"targetLocked"`
	Risk             *int                       `json:"risk"`
	RelatedNodes     int                        `json:"relatedNodes"`
	TotalFlow        *analysis.ExactAmount      `json:"totalFlow"`
	FlowAsset        *string                    `json:"flowAsset"`
	TransactionCount int                        `json:"transactionCount"`
	CurrentResult    *string                    `json:"currentResult"`
	CreatedAt        time.Time                  `json:"createdAt"`
	UpdatedAt        time.Time                  `json:"updatedAt"`
}

type InvestigationHandler struct {
	runs *analysis.RunManager
}

func NewInvestigationHandler(runs *analysis.RunManager) *InvestigationHandler {
	return &InvestigationHandler{runs: runs}
}

func CreateInvestigation(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maximumConversationBodySize)
	var request investigationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "validation_error", "message": "Invalid request"})
		return
	}
	request.Title = strings.TrimSpace(request.Title)
	if request.Title == "" || utf8.RuneCountInString(request.Title) > maxInvestigationTitleLength {
		c.JSON(http.StatusBadRequest, gin.H{"code": "validation_error", "message": "Invalid request"})
		return
	}
	if request.Address != nil {
		*request.Address = strings.TrimSpace(*request.Address)
		if *request.Address == "" {
			request.Address = nil
		} else if !isValidTRONAddress(*request.Address) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_tron_target", "message": "Invalid TRON target"})
			return
		}
	}
	id, err := utils.SecureRandomString(24)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		return
	}
	now := nextInvestigationCreatedAt()
	investigation := &store.Investigation{
		ID:        id,
		OwnerID:   c.GetUint("user_id"),
		Title:     request.Title,
		Address:   request.Address,
		Network:   store.NetworkTRONMainnet,
		Status:    store.InvestigationPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := model.AddInvestigation(investigation); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		return
	}
	c.JSON(http.StatusCreated, newInvestigationDTO(investigation))
}

func nextInvestigationCreatedAt() time.Time {
	for {
		now := time.Now().UTC().UnixMilli()
		previous := lastInvestigationCreatedMillisecond.Load()
		if now <= previous {
			now = previous + 1
		}
		if lastInvestigationCreatedMillisecond.CompareAndSwap(previous, now) {
			return time.UnixMilli(now).UTC()
		}
	}
}

func ListInvestigations(c *gin.Context) {
	investigations, err := model.ListInvestigations(c.GetUint("user_id"), c.Query("query"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		return
	}
	response := make([]investigationDTO, 0, len(investigations))
	for i := range investigations {
		response = append(response, newInvestigationDTO(&investigations[i]))
	}
	c.JSON(http.StatusOK, response)
}

func GetInvestigation(c *gin.Context) {
	investigation, err := model.GetInvestigation(c.GetUint("user_id"), c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "investigation_not_found", "message": "Investigation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		return
	}
	c.JSON(http.StatusOK, newInvestigationDTO(investigation))
}

func UpdateInvestigation(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maximumConversationBodySize)
	var request updateInvestigationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "validation_error", "message": "Invalid request"})
		return
	}
	changes := make(map[string]any)
	if request.Title != nil {
		title := strings.TrimSpace(*request.Title)
		if title == "" || utf8.RuneCountInString(title) > maxInvestigationTitleLength {
			c.JSON(http.StatusBadRequest, gin.H{"code": "validation_error", "message": "Invalid request"})
			return
		}
		changes["title"] = title
	}
	if request.Address != nil {
		address := strings.TrimSpace(*request.Address)
		if address != "" && !isValidTRONAddress(address) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_tron_target", "message": "Invalid TRON target"})
			return
		}
		if address == "" {
			changes["address"] = nil
		} else {
			changes["address"] = address
		}
	}
	investigation, err := model.UpdateInvestigation(c.GetUint("user_id"), c.Param("id"), changes, request.Address != nil)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "investigation_not_found", "message": "Investigation not found"})
			return
		}
		if errors.Is(err, model.ErrInvestigationTargetImmutable) {
			c.JSON(http.StatusConflict, gin.H{"code": "immutable_investigation_target", "message": "Investigation target is immutable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		return
	}
	c.JSON(http.StatusOK, newInvestigationDTO(investigation))
}

func (h *InvestigationHandler) DeleteInvestigation(c *gin.Context) {
	err := h.runs.DeleteInvestigation(c.GetUint("user_id"), c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "investigation_not_found", "message": "Investigation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		return
	}
	c.Status(http.StatusNoContent)
}

func newInvestigationDTO(investigation *store.Investigation) investigationDTO {
	var totalFlow *analysis.ExactAmount
	if investigation.TotalFlowSmallestUnit != nil && investigation.TotalFlowDecimals != nil && investigation.FlowAsset != nil {
		totalFlow = &analysis.ExactAmount{
			SmallestUnit: *investigation.TotalFlowSmallestUnit,
			Decimals:     *investigation.TotalFlowDecimals,
			Asset:        *investigation.FlowAsset,
		}
	}
	return investigationDTO{
		ID:               investigation.ID,
		Title:            investigation.Title,
		Address:          investigation.Address,
		Network:          investigation.Network,
		Status:           investigation.Status,
		TargetLocked:     investigation.TargetLocked,
		Risk:             investigation.RiskScore,
		RelatedNodes:     investigation.RelatedNodes,
		TotalFlow:        totalFlow,
		FlowAsset:        investigation.FlowAsset,
		TransactionCount: investigation.TransactionCount,
		CurrentResult:    investigation.CurrentResultID,
		CreatedAt:        investigation.CreatedAt,
		UpdatedAt:        investigation.UpdatedAt,
	}
}
