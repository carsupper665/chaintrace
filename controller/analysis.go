package controller

import (
	"chaintrace/analysis"
	"chaintrace/model"
	"chaintrace/model/store"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AnalysisHandler struct {
	runs *analysis.RunManager
}

type startAnalysisRunRequest struct {
	TransferLimit  *int `json:"transferLimit"`
	TraversalDepth *int `json:"traversalDepth"`
}

func NewAnalysisHandler(runs *analysis.RunManager) *AnalysisHandler {
	return &AnalysisHandler{runs: runs}
}

func (h *AnalysisHandler) StartRun(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maximumConversationBodySize)
	var request startAnalysisRunRequest
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeInvalidAnalysisScope(c)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeInvalidAnalysisScope(c)
		return
	}
	scope := analysis.DefaultScope()
	if request.TransferLimit != nil {
		scope.TransferLimit = *request.TransferLimit
	}
	if request.TraversalDepth != nil {
		scope.TraversalDepth = *request.TraversalDepth
	}
	if !scope.Valid() {
		writeInvalidAnalysisScope(c)
		return
	}
	investigation, err := model.GetInvestigation(c.GetUint("user_id"), c.Param("id"))
	if err != nil {
		writeInvestigationLookupError(c, err)
		return
	}
	if investigation.Address == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "investigation_target_required", "message": "Investigation target is required"})
		return
	}
	if investigation.Network != store.NetworkTRONMainnet || !IsValidTRONAddress(*investigation.Address) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_tron_target", "message": "Invalid TRON target"})
		return
	}
	run, err := h.runs.Start(c.GetUint("user_id"), *investigation, scope)
	if err != nil {
		if errors.Is(err, analysis.ErrAnalysisRunActive) {
			c.JSON(http.StatusConflict, gin.H{"code": "analysis_run_active", "message": "Analysis run is already active"})
			return
		}
		writeInvestigationLookupError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"id":              run.ID,
		"investigationId": run.InvestigationID,
		"status":          "queued",
		"transferLimit":   run.TransferLimit,
		"traversalDepth":  run.TraversalDepth,
	})
}

func writeInvalidAnalysisScope(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_analysis_scope", "message": "Invalid analysis scope"})
}

func (h *AnalysisHandler) GetRun(c *gin.Context) {
	ownerID := c.GetUint("user_id")
	investigationID := c.Param("id")
	runID := c.Param("runId")
	if _, err := model.GetInvestigation(ownerID, investigationID); err != nil {
		writeInvestigationLookupError(c, err)
		return
	}
	run, err := h.runs.Get(ownerID, investigationID, runID)
	if err != nil {
		if recoveryErr := h.runs.RecoverLost(ownerID, investigationID, runID); recoveryErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"code": "run_lost", "message": "Analysis run was lost"})
		return
	}
	c.JSON(http.StatusOK, run)
}

func (h *AnalysisHandler) CancelRun(c *gin.Context) {
	ownerID := c.GetUint("user_id")
	if _, err := model.GetInvestigation(ownerID, c.Param("id")); err != nil {
		writeInvestigationLookupError(c, err)
		return
	}
	run, err := h.runs.Cancel(ownerID, c.Param("id"), c.Param("runId"))
	if err != nil {
		if errors.Is(err, analysis.ErrAnalysisRunNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "analysis_run_not_found", "message": "Analysis run not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
		return
	}
	c.JSON(http.StatusOK, run)
}

func (h *AnalysisHandler) GetCurrentResult(c *gin.Context) {
	result, err := analysis.LoadCurrentResult(c.GetUint("user_id"), c.Param("id"))
	if err != nil {
		if errors.Is(err, analysis.ErrCurrentResultNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "analysis_result_not_found", "message": "Current analysis result not found"})
			return
		}
		writeInvestigationLookupError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *AnalysisHandler) GetGraph(c *gin.Context) {
	datasetIDs, exists := c.Request.URL.Query()["datasetId"]
	if !exists || len(datasetIDs) != 1 || datasetIDs[0] == "" {
		writeInvalidGraphQuery(c)
		return
	}
	pageSize := 100
	if values, exists := c.Request.URL.Query()["pageSize"]; exists {
		if len(values) != 1 {
			writeInvalidGraphQuery(c)
			return
		}
		parsed, err := strconv.Atoi(values[0])
		if err != nil || parsed < 1 || parsed > 200 {
			writeInvalidGraphQuery(c)
			return
		}
		pageSize = parsed
	}
	cursor := ""
	if values, exists := c.Request.URL.Query()["cursor"]; exists {
		if len(values) != 1 || values[0] == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_graph_cursor", "message": "Invalid graph cursor"})
			return
		}
		cursor = values[0]
	}
	anchor := ""
	if values, exists := c.Request.URL.Query()["anchor"]; exists {
		if len(values) != 1 || values[0] == "" || len(values[0]) > 64 {
			writeInvalidGraphQuery(c)
			return
		}
		anchor = values[0]
	}
	page, err := analysis.LoadGraphPage(c.GetUint("user_id"), c.Param("id"), datasetIDs[0], cursor, anchor, pageSize)
	if err != nil {
		if errors.Is(err, analysis.ErrStaleDataset) {
			c.JSON(http.StatusConflict, gin.H{"code": "stale_dataset", "message": "Analysis Dataset is no longer current"})
			return
		}
		if errors.Is(err, analysis.ErrInvalidGraphCursor) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_graph_cursor", "message": "Invalid graph cursor"})
			return
		}
		writeInvestigationLookupError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func writeInvalidGraphQuery(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_graph_query", "message": "Invalid graph query"})
}

func writeInvestigationLookupError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "investigation_not_found", "message": "Investigation not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "Internal server error"})
}
