package analysis

import (
	"chaintrace/model"
	"chaintrace/model/store"
	"chaintrace/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	AssetUSDT                       = "USDT"
	TRONMainnetUSDTContract         = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	StopReasonSourceExhausted       = "source_exhausted"
	StopReasonTransferLimitReached  = "transfer_limit_reached"
	StopReasonTraversalDepthReached = "traversal_depth_reached"
	StopReasonNoEligibleTransfers   = "no_eligible_transfers"
	StopReasonProviderRateLimited   = "provider_rate_limited"
	StopReasonProviderUnavailable   = "provider_unavailable"
	StopReasonResourceLimitReached  = "resource_limit_reached"
	DefaultTransferLimit            = 500
	DefaultTraversalDepth           = 2
	MaximumTransferLimit            = 5000
	MaximumTraversalDepth           = 4
	analysisRunTimeout              = 30 * time.Minute
	// CollectionWindow is the trailing window collected for both the rules
	// evaluation and the learned score (ADR-0015: they must agree on window).
	CollectionWindow = 30 * 24 * time.Hour
)

var (
	ErrAnalysisRunActive     = errors.New("analysis run is already active")
	ErrAnalysisRunNotFound   = errors.New("analysis run not found")
	ErrCurrentResultNotFound = errors.New("current result not found")
	unsignedIntegerPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
)

type BlockCutoff struct {
	BlockID   string
	Timestamp time.Time
}

type CollectionRequest struct {
	TargetAddress  string
	Network        string
	Asset          string
	WindowStart    time.Time
	WindowEnd      time.Time
	CutoffBlockID  string
	TransferLimit  int
	TraversalDepth int
}

type Scope struct {
	TransferLimit  int `json:"transferLimit"`
	TraversalDepth int `json:"traversalDepth"`
}

func DefaultScope() Scope {
	return Scope{TransferLimit: DefaultTransferLimit, TraversalDepth: DefaultTraversalDepth}
}

func (scope Scope) Valid() bool {
	return scope.TransferLimit >= 1 && scope.TransferLimit <= MaximumTransferLimit &&
		scope.TraversalDepth >= 1 && scope.TraversalDepth <= MaximumTraversalDepth
}

type Collection struct {
	Transactions []BlockchainTransaction
	StopReason   string
	Partial      bool
	Confidence   int
	ReachedDepth int
}

type BlockchainTransaction struct {
	Network        string
	Hash           string
	BlockID        string
	BlockTimestamp time.Time
	Successful     bool
	Confirmed      bool
	Transfers      []TRC20Transfer
}

type TRC20Transfer struct {
	EventIdentity      string
	ContractAddress    string
	Asset              string
	FromAddress        string
	ToAddress          string
	AmountSmallestUnit string
	Decimals           int
	Timestamp          time.Time
}

type ChainDataProvider interface {
	CaptureConfirmedCutoff(context.Context, string) (BlockCutoff, error)
	FetchAddressTransfers(context.Context, AddressTransferPageRequest) (AddressTransferPage, error)
}

type EvaluationInput struct {
	TargetAddress string
	Network       string
	Asset         string
	WindowStart   time.Time
	WindowEnd     time.Time
	Collection    Collection
}

type NodeAssessment struct {
	Address string   `json:"address"`
	Score   int      `json:"score"`
	Level   string   `json:"level"`
	Reasons []string `json:"reasons"`
}

type Evaluation struct {
	Score           *int
	Level           string
	Reasons         []string
	NodeAssessments []NodeAssessment
	Source          string
}

type RiskEvaluator interface {
	Evaluate(context.Context, EvaluationInput) (Evaluation, error)
}

type Options struct {
	Provider     ChainDataProvider
	Evaluator    RiskEvaluator
	// LearnedScorer is nil when no learned scorer is configured, which means
	// skip it — unlike Evaluator, nil is never replaced with a default; there
	// is no rules-equivalent fallback for a learned score.
	LearnedScorer LearnedScorer
	LaunchWorker  func(func())
}

type Run struct {
	ID                 string  `json:"id"`
	InvestigationID    string  `json:"investigationId"`
	Status             string  `json:"status"`
	Phase              string  `json:"phase"`
	TransferLimit      int     `json:"transferLimit"`
	TraversalDepth     int     `json:"traversalDepth"`
	CollectedTransfers int     `json:"collectedTransfers"`
	ResultID           *string `json:"resultId,omitempty"`
	ErrorCode          *string `json:"errorCode,omitempty"`

	ownerID uint
}

type RunManager struct {
	mu            sync.RWMutex
	runs          map[string]*managedRun
	active        map[string]string
	provider      ChainDataProvider
	evaluator     RiskEvaluator
	learnedScorer LearnedScorer
	launchWorker  func(func())
}

type managedRun struct {
	run    Run
	ctx    context.Context
	cancel context.CancelFunc
	// publishing is non-nil while a worker holds the terminal-transition claim
	// and is committing its Analysis Dataset. It is closed once that worker has
	// recorded the terminal state. Cancellation waits on it instead of holding
	// the manager mutex across the publication transaction, so polling stays
	// responsive while exactly one of publication and cancellation still wins.
	publishing chan struct{}
}

func NewRunManager(options Options) *RunManager {
	evaluator := options.Evaluator
	if evaluator == nil {
		evaluator = RulesV1Evaluator{}
	}
	return &RunManager{
		runs:          make(map[string]*managedRun),
		active:        make(map[string]string),
		provider:      options.Provider,
		evaluator:     evaluator,
		learnedScorer: options.LearnedScorer,
		launchWorker:  options.LaunchWorker,
	}
}

func (m *RunManager) Start(ownerID uint, investigation store.Investigation, scope Scope) (Run, error) {
	if !scope.Valid() {
		return Run{}, errors.New("invalid analysis scope")
	}
	runID, err := utils.SecureRandomString(24)
	if err != nil {
		return Run{}, err
	}
	m.mu.Lock()
	if _, exists := m.active[investigation.ID]; exists {
		m.mu.Unlock()
		return Run{}, ErrAnalysisRunActive
	}
	result := model.DB.Model(&store.Investigation{}).
		Where(
			"owner_id = ? AND id = ? AND (active_analysis_run_id IS NULL OR active_analysis_run_id = ?)",
			ownerID, investigation.ID, "",
		).
		Updates(map[string]any{
			"status":                 store.InvestigationAnalyzing,
			"active_analysis_run_id": runID,
		})
	if result.Error != nil {
		m.mu.Unlock()
		return Run{}, result.Error
	}
	if result.RowsAffected == 0 {
		m.mu.Unlock()
		var existing int64
		if err := model.DB.Model(&store.Investigation{}).
			Where("owner_id = ? AND id = ?", ownerID, investigation.ID).
			Count(&existing).Error; err != nil {
			return Run{}, err
		}
		if existing > 0 {
			return Run{}, ErrAnalysisRunActive
		}
		return Run{}, gorm.ErrRecordNotFound
	}
	run := Run{
		ID:              runID,
		InvestigationID: investigation.ID,
		Status:          "queued",
		Phase:           "queued",
		TransferLimit:   scope.TransferLimit,
		TraversalDepth:  scope.TraversalDepth,
		ownerID:         ownerID,
	}
	ctx, cancel := context.WithTimeout(context.Background(), analysisRunTimeout)
	m.runs[runID] = &managedRun{run: run, ctx: ctx, cancel: cancel}
	m.active[investigation.ID] = runID
	accepted := run
	m.mu.Unlock()

	worker := func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				if utils.SysLog != nil {
					utils.SysLog.Errorf("analysis run %s panicked: %v", runID, recovered)
				}
				m.fail(investigation, runID, "internal_error")
			}
		}()
		m.execute(ctx, investigation, runID, scope)
	}
	if m.launchWorker != nil {
		m.launchWorker(worker)
	} else {
		go worker()
	}
	return accepted, nil
}

func (m *RunManager) Get(ownerID uint, investigationID, runID string) (Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	managed, exists := m.runs[runID]
	if !exists || managed.run.ownerID != ownerID || managed.run.InvestigationID != investigationID {
		return Run{}, ErrAnalysisRunNotFound
	}
	return cloneRun(&managed.run), nil
}

func (m *RunManager) RecoverLost(ownerID uint, investigationID, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if managed := m.runs[runID]; managed != nil && managed.run.ownerID == ownerID && managed.run.InvestigationID == investigationID {
		return nil
	}
	return restoreStableStatus(ownerID, investigationID, runID)
}

// awaitPublicationLocked returns the run once no worker holds the terminal
// claim for it. m.mu must be held on entry and is held again on return, but it
// is released while waiting.
func (m *RunManager) awaitPublicationLocked(runID string) *managedRun {
	for {
		managed := m.runs[runID]
		if managed == nil || managed.publishing == nil {
			return managed
		}
		claim := managed.publishing
		m.mu.Unlock()
		<-claim
		m.mu.Lock()
	}
}

func (m *RunManager) Cancel(ownerID uint, investigationID, runID string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	managed := m.awaitPublicationLocked(runID)
	if managed == nil || managed.run.ownerID != ownerID || managed.run.InvestigationID != investigationID {
		return Run{}, ErrAnalysisRunNotFound
	}
	if managed.run.Status != "queued" && managed.run.Status != "running" {
		return cloneRun(&managed.run), nil
	}
	if err := m.cancelLocked(managed); err != nil {
		return Run{}, err
	}
	return cloneRun(&managed.run), nil
}

func (m *RunManager) DeleteInvestigation(ownerID uint, investigationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for {
		runID, exists := m.active[investigationID]
		if !exists {
			break
		}
		managed := m.awaitPublicationLocked(runID)
		// A publication may have finished and released the Investigation while
		// the mutex was released above.
		if current, still := m.active[investigationID]; !still || current != runID {
			continue
		}
		if managed != nil && managed.run.ownerID == ownerID {
			if err := m.cancelLocked(managed); err != nil {
				return err
			}
		}
		break
	}
	return model.DeleteInvestigation(ownerID, investigationID)
}

func (m *RunManager) cancelLocked(managed *managedRun) error {
	managed.cancel()
	err := restoreStableStatus(managed.run.ownerID, managed.run.InvestigationID, managed.run.ID)
	managed.run.Status = "cancelled"
	managed.run.Phase = "cancelled"
	delete(m.active, managed.run.InvestigationID)
	return err
}

func (m *RunManager) execute(ctx context.Context, investigation store.Investigation, runID string, scope Scope) {
	if ctx.Err() != nil {
		return
	}
	m.progress(runID, "running", "capturing_cutoff", 0)
	if m.provider == nil {
		m.fail(investigation, runID, "provider_unavailable")
		return
	}
	cutoff, err := m.provider.CaptureConfirmedCutoff(ctx, string(investigation.Network))
	if err != nil || cutoff.BlockID == "" || cutoff.Timestamp.IsZero() {
		code := "provider_failure"
		var interruption *CollectionInterruption
		if errors.As(err, &interruption) {
			if stopReason := interruptionStopReason(interruption.Kind); stopReason != "" {
				code = stopReason
			}
		}
		m.fail(investigation, runID, code)
		return
	}
	cutoff.Timestamp = cutoff.Timestamp.UTC()
	request := CollectionRequest{
		TargetAddress:  *investigation.Address,
		Network:        string(investigation.Network),
		Asset:          AssetUSDT,
		WindowStart:    cutoff.Timestamp.Add(-CollectionWindow),
		WindowEnd:      cutoff.Timestamp,
		CutoffBlockID:  cutoff.BlockID,
		TransferLimit:  scope.TransferLimit,
		TraversalDepth: scope.TraversalDepth,
	}
	m.progress(runID, "running", "collecting", 0)
	collection, err := NewCollector(m.provider).Collect(ctx, request)
	if err != nil {
		code := "provider_failure"
		var interruption *CollectionInterruption
		if errors.As(err, &interruption) {
			if stopReason := interruptionStopReason(interruption.Kind); stopReason != "" {
				code = stopReason
			}
		}
		m.fail(investigation, runID, code)
		return
	}
	collected := transferCount(collection)
	m.progress(runID, "running", "evaluating", collected)
	evaluation, err := m.evaluator.Evaluate(ctx, EvaluationInput{
		TargetAddress: *investigation.Address,
		Network:       string(investigation.Network),
		Asset:         AssetUSDT,
		WindowStart:   request.WindowStart,
		WindowEnd:     request.WindowEnd,
		Collection:    collection,
	})
	if err != nil {
		m.fail(investigation, runID, "evaluation_failed")
		return
	}
	learnedScore, learnedScoreSource := m.attemptLearnedScore(ctx, request)
	m.progress(runID, "running", "publishing", collected)
	m.publish(investigation, runID, request, collection, evaluation, learnedScore, learnedScoreSource, collected)
}

func (m *RunManager) progress(runID, status, phase string, collected int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if managed := m.runs[runID]; managed != nil && (managed.run.Status == "queued" || managed.run.Status == "running") {
		managed.run.Status = status
		managed.run.Phase = phase
		if collected > managed.run.CollectedTransfers {
			managed.run.CollectedTransfers = collected
		}
	}
}

func (m *RunManager) fail(investigation store.Investigation, runID, code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	managed := m.runs[runID]
	if managed == nil || (managed.run.Status != "queued" && managed.run.Status != "running") {
		return
	}
	logRestoreFailure(investigation, runID, restoreStableStatus(investigation.OwnerID, investigation.ID, runID))
	managed.cancel()
	managed.run.Status = "failed"
	managed.run.Phase = "failed"
	managed.run.ErrorCode = &code
	delete(m.active, managed.run.InvestigationID)
}

func (m *RunManager) publish(investigation store.Investigation, runID string, request CollectionRequest, collection Collection, evaluation Evaluation, learnedScore *float64, learnedScoreSource string, collected int) {
	// Claim the terminal transition under the mutex, then commit without it.
	// Holding the mutex across the publication transaction would block every
	// poll for as long as the Dataset takes to write.
	m.mu.Lock()
	managed := m.runs[runID]
	if managed == nil || managed.publishing != nil ||
		(managed.run.Status != "queued" && managed.run.Status != "running") || managed.ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	claim := make(chan struct{})
	managed.publishing = claim
	m.mu.Unlock()

	datasetID, err := publish(investigation, runID, request, collection, evaluation, learnedScore, learnedScoreSource)

	m.mu.Lock()
	defer m.mu.Unlock()
	// Released before the mutex, so a waiter only proceeds once the terminal
	// state below is visible.
	defer close(claim)
	defer func() { managed.publishing = nil }()
	if err != nil {
		logRestoreFailure(investigation, runID, restoreStableStatus(investigation.OwnerID, investigation.ID, runID))
		code := "publication_failed"
		managed.cancel()
		managed.run.Status = "failed"
		managed.run.Phase = "failed"
		managed.run.ErrorCode = &code
		delete(m.active, managed.run.InvestigationID)
		return
	}
	managed.cancel()
	managed.run.Status = "completed"
	managed.run.Phase = "completed"
	managed.run.CollectedTransfers = collected
	managed.run.ResultID = &datasetID
	delete(m.active, managed.run.InvestigationID)
}

func logRestoreFailure(investigation store.Investigation, runID string, err error) {
	if err != nil && utils.SysLog != nil {
		utils.SysLog.Errorf("investigation %s left analyzing after run %s: %v", investigation.ID, runID, err)
	}
}

func restoreStableStatus(ownerID uint, investigationID, runID string) error {
	return model.DB.Model(&store.Investigation{}).
		Where("owner_id = ? AND id = ? AND active_analysis_run_id = ?", ownerID, investigationID, runID).
		Updates(map[string]any{
			"status": gorm.Expr(
				"CASE WHEN current_result_id IS NULL THEN ? ELSE ? END",
				store.InvestigationPending,
				store.InvestigationCompleted,
			),
			"active_analysis_run_id": nil,
		}).Error
}

func cloneRun(run *Run) Run {
	result := *run
	if run.ResultID != nil {
		value := *run.ResultID
		result.ResultID = &value
	}
	if run.ErrorCode != nil {
		value := *run.ErrorCode
		result.ErrorCode = &value
	}
	return result
}

type ExactAmount struct {
	SmallestUnit string `json:"smallestUnit"`
	Decimals     int    `json:"decimals"`
	Asset        string `json:"asset"`
}

type DatasetResult struct {
	ID                 string                     `json:"id"`
	Network            store.InvestigationNetwork `json:"network"`
	Asset              string                     `json:"asset"`
	WindowStart        time.Time                  `json:"windowStart"`
	WindowEnd          time.Time                  `json:"windowEnd"`
	CutoffBlockID      string                     `json:"cutoffBlockId"`
	TransferLimit      int                        `json:"transferLimit"`
	TraversalDepth     int                        `json:"traversalDepth"`
	CollectedTransfers int                        `json:"collectedTransfers"`
	ReachedDepth       int                        `json:"reachedDepth"`
	Partial            bool                       `json:"partial"`
	Confidence         int                        `json:"confidence"`
	StopReason         string                     `json:"stopReason"`
	CreatedAt          time.Time                  `json:"createdAt"`
}

type MetricsResult struct {
	RelatedNodes  int         `json:"relatedNodes"`
	TransferCount int         `json:"transferCount"`
	TotalFlow     ExactAmount `json:"totalFlow"`
}

type AssessmentResult struct {
	Score              *int             `json:"score"`
	Level              string           `json:"level"`
	Reasons            []string         `json:"reasons"`
	NodeAssessments    []NodeAssessment `json:"nodeAssessments"`
	Source             string           `json:"source"`
	LearnedScore       *float64         `json:"learnedScore"`
	LearnedScoreSource string           `json:"learnedScoreSource"`
	UpdatedAt          time.Time        `json:"updatedAt"`
}

type CurrentResult struct {
	Dataset    DatasetResult    `json:"dataset"`
	Metrics    MetricsResult    `json:"metrics"`
	Assessment AssessmentResult `json:"assessment"`
}

func LoadCurrentResult(ownerID uint, investigationID string) (CurrentResult, error) {
	var current CurrentResult
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var investigation store.Investigation
		if err := tx.Where("owner_id = ? AND id = ?", ownerID, investigationID).First(&investigation).Error; err != nil {
			return err
		}
		if investigation.CurrentResultID == nil {
			return ErrCurrentResultNotFound
		}
		var dataset store.AnalysisDataset
		if err := tx.First(&dataset, "id = ? AND investigation_id = ?", *investigation.CurrentResultID, investigationID).Error; err != nil {
			return err
		}
		var metrics store.InvestigationMetrics
		if err := tx.First(&metrics, "dataset_id = ?", dataset.ID).Error; err != nil {
			return err
		}
		var assessment store.Assessment
		if err := tx.First(&assessment, "dataset_id = ?", dataset.ID).Error; err != nil {
			return err
		}
		var reasons []string
		if err := json.Unmarshal([]byte(assessment.ReasonsJSON), &reasons); err != nil {
			return err
		}
		var nodes []NodeAssessment
		if err := json.Unmarshal([]byte(assessment.NodeAssessmentsJSON), &nodes); err != nil {
			return err
		}
		current = CurrentResult{
			Dataset: DatasetResult{
				ID:                 dataset.ID,
				Network:            dataset.Network,
				Asset:              dataset.Asset,
				WindowStart:        dataset.WindowStart,
				WindowEnd:          dataset.WindowEnd,
				CutoffBlockID:      dataset.CutoffBlockID,
				TransferLimit:      dataset.TransferLimit,
				TraversalDepth:     dataset.TraversalDepth,
				CollectedTransfers: dataset.CollectedTransfers,
				ReachedDepth:       dataset.ReachedDepth,
				Partial:            dataset.Partial,
				Confidence:         dataset.Confidence,
				StopReason:         dataset.StopReason,
				CreatedAt:          dataset.CreatedAt,
			},
			Metrics: MetricsResult{
				RelatedNodes:  metrics.RelatedNodes,
				TransferCount: metrics.TransferCount,
				TotalFlow: ExactAmount{
					SmallestUnit: metrics.TotalFlowSmallestUnit,
					Decimals:     metrics.TotalFlowDecimals,
					Asset:        metrics.Asset,
				},
			},
			Assessment: AssessmentResult{
				Score:              assessment.Score,
				Level:              assessment.Level,
				Reasons:            reasons,
				NodeAssessments:    nodes,
				Source:             assessment.Source,
				LearnedScore:       assessment.LearnedScore,
				LearnedScoreSource: assessment.LearnedScoreSource,
				UpdatedAt:          assessment.UpdatedAt,
			},
		}
		return nil
	})
	return current, err
}

type calculatedMetrics struct {
	relatedNodes  int
	transferCount int
	totalFlow     string
	decimals      int
	asset         string
}

func publish(investigation store.Investigation, runID string, request CollectionRequest, collection Collection, evaluation Evaluation, learnedScore *float64, learnedScoreSource string) (string, error) {
	metrics, err := calculateMetrics(*investigation.Address, collection)
	if err != nil {
		return "", err
	}
	if collection.StopReason == "" || collection.Confidence < 0 || collection.Confidence > 100 {
		return "", errors.New("invalid analysis coverage")
	}
	if evaluation.Source == "" {
		return "", errors.New("invalid evaluation")
	}
	if metrics.transferCount == 0 {
		if evaluation.Score != nil || evaluation.Level != "" || collection.Confidence != 0 {
			return "", errors.New("invalid insufficient-evidence evaluation")
		}
	} else if evaluation.Score == nil || *evaluation.Score < 0 || *evaluation.Score > 100 || evaluation.Level == "" {
		return "", errors.New("invalid evaluation")
	}
	datasetID, err := utils.SecureRandomString(24)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	reasonsJSON, err := json.Marshal(nonNilStrings(evaluation.Reasons))
	if err != nil {
		return "", err
	}
	nodesJSON, err := json.Marshal(nonNilNodes(evaluation.NodeAssessments))
	if err != nil {
		return "", err
	}
	dataset := store.AnalysisDataset{
		ID:                 datasetID,
		InvestigationID:    investigation.ID,
		AnalysisRunID:      runID,
		Network:            investigation.Network,
		Asset:              AssetUSDT,
		CutoffBlockID:      request.CutoffBlockID,
		WindowStart:        request.WindowStart,
		WindowEnd:          request.WindowEnd,
		TransferLimit:      request.TransferLimit,
		TraversalDepth:     request.TraversalDepth,
		CollectedTransfers: metrics.transferCount,
		ReachedDepth:       collection.ReachedDepth,
		Partial:            collection.Partial,
		Confidence:         collection.Confidence,
		StopReason:         collection.StopReason,
		CreatedAt:          now,
	}
	// Build and validate the evidence rows before opening the transaction so the
	// publication holds its database transaction for as short as possible.
	transactions, transfers, err := datasetEvidence(investigation, datasetID, now, collection)
	if err != nil {
		return "", err
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&dataset).Error; err != nil {
			return err
		}
		if len(transactions) > 0 {
			if err := tx.CreateInBatches(transactions, evidenceInsertBatchSize).Error; err != nil {
				return err
			}
		}
		if len(transfers) > 0 {
			if err := tx.CreateInBatches(transfers, evidenceInsertBatchSize).Error; err != nil {
				return err
			}
		}
		storedMetrics := store.InvestigationMetrics{
			DatasetID:             datasetID,
			RelatedNodes:          metrics.relatedNodes,
			TransferCount:         metrics.transferCount,
			TotalFlowSmallestUnit: metrics.totalFlow,
			TotalFlowDecimals:     metrics.decimals,
			Asset:                 metrics.asset,
			CreatedAt:             now,
		}
		if err := tx.Create(&storedMetrics).Error; err != nil {
			return err
		}
		assessment := store.Assessment{
			DatasetID:           datasetID,
			Score:               evaluation.Score,
			Level:               evaluation.Level,
			ReasonsJSON:         string(reasonsJSON),
			NodeAssessmentsJSON: string(nodesJSON),
			Source:              evaluation.Source,
			LearnedScore:        learnedScore,
			LearnedScoreSource:  learnedScoreSource,
			UpdatedAt:           now,
		}
		if err := tx.Create(&assessment).Error; err != nil {
			return err
		}
		flowAsset := metrics.asset
		flowDecimals := metrics.decimals
		var riskScore any
		if evaluation.Score != nil {
			riskScore = *evaluation.Score
		}
		updates := map[string]any{
			"status":                   store.InvestigationCompleted,
			"active_analysis_run_id":   nil,
			"target_locked":            true,
			"current_result_id":        datasetID,
			"risk_score":               riskScore,
			"related_nodes":            metrics.relatedNodes,
			"total_flow_smallest_unit": metrics.totalFlow,
			"total_flow_decimals":      flowDecimals,
			"flow_asset":               flowAsset,
			"transaction_count":        metrics.transferCount,
		}
		result := tx.Model(&store.Investigation{}).
			Where("owner_id = ? AND id = ? AND status = ? AND active_analysis_run_id = ?", investigation.OwnerID, investigation.ID, store.InvestigationAnalyzing, runID).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("investigation is not publishable")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return datasetID, nil
}

// evidenceInsertBatchSize keeps the bounded Dataset (up to MaximumTransferLimit
// Transfers) within a handful of multi-row inserts.
const evidenceInsertBatchSize = 200

// datasetEvidence normalizes and validates one collection into the rows that
// belong to datasetID. It performs no database work.
func datasetEvidence(investigation store.Investigation, datasetID string, now time.Time, collection Collection) ([]store.BlockchainTransaction, []store.TRC20Transfer, error) {
	transactions := make([]store.BlockchainTransaction, 0, len(collection.Transactions))
	transfers := make([]store.TRC20Transfer, 0, len(collection.Transactions))
	for _, sourceTransaction := range collection.Transactions {
		transactionID, err := utils.SecureRandomString(24)
		if err != nil {
			return nil, nil, err
		}
		transaction := store.BlockchainTransaction{
			ID:              transactionID,
			DatasetID:       datasetID,
			Network:         store.InvestigationNetwork(sourceTransaction.Network),
			TransactionHash: sourceTransaction.Hash,
			BlockID:         sourceTransaction.BlockID,
			BlockTimestamp:  sourceTransaction.BlockTimestamp,
			Successful:      sourceTransaction.Successful,
			Confirmed:       sourceTransaction.Confirmed,
			CreatedAt:       now,
		}
		if err := validateTransaction(investigation.Network, transaction); err != nil {
			return nil, nil, err
		}
		transactions = append(transactions, transaction)
		for _, sourceTransfer := range sourceTransaction.Transfers {
			transferID, err := utils.SecureRandomString(24)
			if err != nil {
				return nil, nil, err
			}
			transfer := store.TRC20Transfer{
				ID:                      transferID,
				DatasetID:               datasetID,
				BlockchainTransactionID: transactionID,
				TransactionHash:         sourceTransaction.Hash,
				EventIdentity:           sourceTransfer.EventIdentity,
				ContractAddress:         sourceTransfer.ContractAddress,
				Asset:                   sourceTransfer.Asset,
				FromAddress:             sourceTransfer.FromAddress,
				ToAddress:               sourceTransfer.ToAddress,
				AmountSmallestUnit:      sourceTransfer.AmountSmallestUnit,
				Decimals:                sourceTransfer.Decimals,
				Timestamp:               sourceTransfer.Timestamp,
				CreatedAt:               now,
			}
			if err := validateTransfer(transfer); err != nil {
				return nil, nil, err
			}
			transfers = append(transfers, transfer)
		}
	}
	return transactions, transfers, nil
}

func calculateMetrics(target string, collection Collection) (calculatedMetrics, error) {
	total := new(big.Int)
	peers := make(map[string]struct{})
	metrics := calculatedMetrics{asset: AssetUSDT, decimals: 6}
	for _, transaction := range collection.Transactions {
		for _, transfer := range transaction.Transfers {
			if !unsignedIntegerPattern.MatchString(transfer.AmountSmallestUnit) {
				return calculatedMetrics{}, fmt.Errorf("invalid transfer amount %q", transfer.AmountSmallestUnit)
			}
			amount, ok := new(big.Int).SetString(transfer.AmountSmallestUnit, 10)
			if !ok {
				return calculatedMetrics{}, errors.New("invalid transfer amount")
			}
			if metrics.transferCount == 0 {
				metrics.decimals = transfer.Decimals
				metrics.asset = transfer.Asset
			} else if transfer.Decimals != metrics.decimals || transfer.Asset != metrics.asset {
				return calculatedMetrics{}, errors.New("mixed transfer units")
			}
			total.Add(total, amount)
			metrics.transferCount++
			if transfer.FromAddress != target {
				peers[transfer.FromAddress] = struct{}{}
			}
			if transfer.ToAddress != target {
				peers[transfer.ToAddress] = struct{}{}
			}
		}
	}
	metrics.relatedNodes = len(peers)
	metrics.totalFlow = total.String()
	return metrics, nil
}

func validateTransaction(network store.InvestigationNetwork, transaction store.BlockchainTransaction) error {
	if transaction.Network != network || transaction.TransactionHash == "" || transaction.BlockID == "" || transaction.BlockTimestamp.IsZero() || !transaction.Successful || !transaction.Confirmed {
		return errors.New("ineligible blockchain transaction")
	}
	return nil
}

func validateTransfer(transfer store.TRC20Transfer) error {
	if transfer.EventIdentity == "" || transfer.ContractAddress != TRONMainnetUSDTContract || transfer.Asset != AssetUSDT || transfer.FromAddress == "" || transfer.ToAddress == "" || transfer.Decimals != 6 || transfer.Timestamp.IsZero() || !unsignedIntegerPattern.MatchString(transfer.AmountSmallestUnit) {
		return errors.New("invalid TRC20 USDT transfer")
	}
	return nil
}

func transferCount(collection Collection) int {
	count := 0
	for _, transaction := range collection.Transactions {
		count += len(transaction.Transfers)
	}
	return count
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilNodes(values []NodeAssessment) []NodeAssessment {
	if values == nil {
		return []NodeAssessment{}
	}
	return values
}
