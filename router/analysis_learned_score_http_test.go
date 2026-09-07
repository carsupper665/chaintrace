package router_test

import (
	"chaintrace/analysis"
	"chaintrace/model"
	"chaintrace/model/store"
	apiRouter "chaintrace/router"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// fixtureLearnedScorer is a test double for analysis.LearnedScorer.
type fixtureLearnedScorer struct {
	result analysis.LearnedScoreResult
	err    error
}

func (s fixtureLearnedScorer) Score(context.Context, analysis.LearnedScoreInput) (analysis.LearnedScoreResult, error) {
	return s.result, s.err
}

type currentResultAssessment struct {
	Score              *int     `json:"score"`
	Level              string   `json:"level"`
	Reasons            []string `json:"reasons"`
	Source             string   `json:"source"`
	LearnedScore       *float64 `json:"learnedScore"`
	LearnedScoreSource string   `json:"learnedScoreSource"`
}

func fetchCurrentAssessment(t *testing.T, test *authHTTPTest, token, investigationID string) currentResultAssessment {
	t.Helper()
	response := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/current-result", nil, token)
	if response.Code != http.StatusOK {
		t.Fatalf("current-result status = %d; body: %s", response.Code, response.Body.String())
	}
	var decoded struct {
		Assessment currentResultAssessment `json:"assessment"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode current result: %v", err)
	}
	return decoded.Assessment
}

func startAndCompleteRun(t *testing.T, test *authHTTPTest, token, investigationID string) analysisRunResponse {
	t.Helper()
	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if started.Code != http.StatusAccepted {
		t.Fatalf("start status = %d; body: %s", started.Code, started.Body.String())
	}
	return pollAnalysisRun(t, test, token, investigationID, decodeRunID(t, started.Body.Bytes()))
}

// TestLearnedScorerErrorLeavesRunCompletedRulesOnly is the literal Phase 3
// acceptance criterion: a scorer failure (here, a fixture standing in for a
// 5xx or a malformed body — decodeScoreResponse in scoreclient collapses all
// of those to the same error) must not fail the run, and the rules-side
// assessment must be exactly what it would be with no scorer configured.
func TestLearnedScorerErrorLeavesRunCompletedRulesOnly(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:      successfulProvider(cutoff),
		Evaluator:     successfulEvaluator(),
		LearnedScorer: fixtureLearnedScorer{err: errors.New("fixture scorer 5xx")},
	})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	completed := startAndCompleteRun(t, test, token, investigationID)
	if completed.Status != "completed" || completed.ResultID == nil {
		t.Fatalf("run with failing scorer = %#v", completed)
	}

	assessment := fetchCurrentAssessment(t, test, token, investigationID)
	if assessment.Score == nil || *assessment.Score != 0 || assessment.Level != "low" || assessment.Source != "rules-v1" {
		t.Errorf("rules assessment changed by a failing scorer: %#v", assessment)
	}
	if assessment.LearnedScore != nil || assessment.LearnedScoreSource != "" {
		t.Errorf("learnedScore = %v/%q, want nil/\"\" when the scorer errors", assessment.LearnedScore, assessment.LearnedScoreSource)
	}

	var stored store.Assessment
	if err := model.DB.First(&stored, "dataset_id = ?", *completed.ResultID).Error; err != nil {
		t.Fatalf("load stored assessment: %v", err)
	}
	if stored.LearnedScore != nil || stored.LearnedScoreSource != "" {
		t.Errorf("stored LearnedScore = %v/%q, want nil/\"\"", stored.LearnedScore, stored.LearnedScoreSource)
	}
}

// TestLearnedScoreFetchFailureLeavesRunCompletedRulesOnly proves the
// independent fetch path specifically (not just the scorer HTTP call): the
// learned-score fetch always asks with TraversalDepth 1, which the rules
// path's own collection (TraversalDepth 2 by default) never does, so failing
// only depth-1 requests isolates it.
func TestLearnedScoreFetchFailureLeavesRunCompletedRulesOnly(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	provider := successfulProvider(cutoff)
	provider.fetch = func(ctx context.Context, request analysis.AddressTransferPageRequest) (analysis.AddressTransferPage, error) {
		if request.TraversalDepth == 1 {
			return analysis.AddressTransferPage{}, errors.New("fixture: learned-score fetch failed")
		}
		if request.Address != testTargetAddress || request.Cursor != "" {
			return analysis.AddressTransferPage{}, nil
		}
		collection, err := provider.Collect(ctx, analysis.CollectionRequest{
			TargetAddress: request.Address, Network: request.Network, Asset: request.Asset,
			WindowStart: request.WindowStart, WindowEnd: request.WindowEnd,
			CutoffBlockID: request.CutoffBlockID, TransferLimit: request.TransferLimit,
			TraversalDepth: request.TraversalDepth,
		})
		if err != nil {
			return analysis.AddressTransferPage{}, err
		}
		return analysis.AddressTransferPage{Transactions: collection.Transactions}, nil
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  provider,
		Evaluator: successfulEvaluator(),
		LearnedScorer: fixtureLearnedScorer{
			result: analysis.LearnedScoreResult{Score: -0.9, Source: "should-not-be-reached"},
		},
	})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	completed := startAndCompleteRun(t, test, token, investigationID)
	if completed.Status != "completed" || completed.ResultID == nil {
		t.Fatalf("run with failing learned-score fetch = %#v", completed)
	}

	assessment := fetchCurrentAssessment(t, test, token, investigationID)
	if assessment.Score == nil || *assessment.Score != 0 || assessment.Source != "rules-v1" {
		t.Errorf("rules assessment changed by a failing fetch: %#v", assessment)
	}
	if assessment.LearnedScore != nil {
		t.Errorf("learnedScore = %v, want nil when its own fetch fails", assessment.LearnedScore)
	}
}

// TestSuccessfulLearnedScoreIsRecordedBesideRulesScore is acceptance #1 and
// #2 together: both scores land in the dataset, and the rules-visible score
// is byte-identical to a run with no scorer configured at all.
func TestSuccessfulLearnedScoreIsRecordedBesideRulesScore(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  successfulProvider(cutoff),
		Evaluator: successfulEvaluator(),
		LearnedScorer: fixtureLearnedScorer{
			result: analysis.LearnedScoreResult{Score: -0.42, Source: "manifest-hash-abc123"},
		},
	})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	completed := startAndCompleteRun(t, test, token, investigationID)
	if completed.Status != "completed" || completed.ResultID == nil {
		t.Fatalf("run with a successful scorer = %#v", completed)
	}

	assessment := fetchCurrentAssessment(t, test, token, investigationID)
	if assessment.Score == nil || *assessment.Score != 0 || assessment.Level != "low" || assessment.Source != "rules-v1" {
		t.Errorf("rules assessment = %#v, want the unchanged rules-only result", assessment)
	}
	if assessment.LearnedScore == nil || *assessment.LearnedScore != -0.42 || assessment.LearnedScoreSource != "manifest-hash-abc123" {
		t.Errorf("learnedScore = %v/%q, want -0.42/manifest-hash-abc123", assessment.LearnedScore, assessment.LearnedScoreSource)
	}

	var stored store.Assessment
	if err := model.DB.First(&stored, "dataset_id = ?", *completed.ResultID).Error; err != nil {
		t.Fatalf("load stored assessment: %v", err)
	}
	if stored.LearnedScore == nil || *stored.LearnedScore != -0.42 || stored.LearnedScoreSource != "manifest-hash-abc123" {
		t.Errorf("stored assessment = %#v, want LearnedScore -0.42 / manifest-hash-abc123", stored)
	}
}

// TestNilLearnedScorerBehavesExactlyAsBefore pins the no-op path: no
// LearnedScorer configured (the zero value, same as every test written
// before Phase 3) must not touch the provider at all for the learned-score
// fetch, and learnedScore must read back as null.
func TestNilLearnedScorerBehavesExactlyAsBefore(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	provider := successfulProvider(cutoff)
	var depthOneFetchCalls atomic.Int32
	provider.fetch = func(ctx context.Context, request analysis.AddressTransferPageRequest) (analysis.AddressTransferPage, error) {
		if request.TraversalDepth == 1 {
			depthOneFetchCalls.Add(1)
		}
		if request.Address != testTargetAddress || request.Cursor != "" {
			return analysis.AddressTransferPage{}, nil
		}
		collection, err := provider.Collect(ctx, analysis.CollectionRequest{
			TargetAddress: request.Address, Network: request.Network, Asset: request.Asset,
			WindowStart: request.WindowStart, WindowEnd: request.WindowEnd,
			CutoffBlockID: request.CutoffBlockID, TransferLimit: request.TransferLimit,
			TraversalDepth: request.TraversalDepth,
		})
		if err != nil {
			return analysis.AddressTransferPage{}, err
		}
		return analysis.AddressTransferPage{Transactions: collection.Transactions}, nil
	}
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider: provider, Evaluator: successfulEvaluator(),
	})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	completed := startAndCompleteRun(t, test, token, investigationID)
	if completed.Status != "completed" || completed.ResultID == nil {
		t.Fatalf("run with no learned scorer = %#v", completed)
	}

	assessment := fetchCurrentAssessment(t, test, token, investigationID)
	if assessment.LearnedScore != nil || assessment.LearnedScoreSource != "" {
		t.Errorf("learnedScore = %v/%q, want nil/\"\" with no scorer configured", assessment.LearnedScore, assessment.LearnedScoreSource)
	}
	// TraversalDepth 1 is the learned-score fetch's own signature (the rules
	// path always sends the scope's own TraversalDepth, 2 by default) — it
	// must never be asked for at all when attemptLearnedScore returns early.
	if depthOneFetchCalls.Load() != 0 {
		t.Errorf("depth-1 fetch calls = %d, want 0 with no scorer configured", depthOneFetchCalls.Load())
	}
}
