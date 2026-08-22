package router_test

import (
	"chaintrace/analysis"
	"chaintrace/model"
	"chaintrace/model/store"
	apiRouter "chaintrace/router"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type lifecycleEvaluator struct {
	evaluate func(context.Context, analysis.EvaluationInput) (analysis.Evaluation, error)
}

func (e lifecycleEvaluator) Evaluate(ctx context.Context, input analysis.EvaluationInput) (analysis.Evaluation, error) {
	return e.evaluate(ctx, input)
}

func TestOwnerCanCancelQueuedAnalysisRunIdempotently(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	provider := successfulProvider(cutoff)
	var captureCalls atomic.Int32
	provider.capture = func(context.Context, string) (analysis.BlockCutoff, error) {
		captureCalls.Add(1)
		return provider.cutoff, nil
	}
	queuedWorker := make(chan func(), 1)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  provider,
		Evaluator: successfulEvaluator(),
		LaunchWorker: func(worker func()) {
			queuedWorker <- worker
		},
	})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)

	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if started.Code != http.StatusAccepted {
		t.Fatalf("queued start status = %d; body: %s", started.Code, started.Body.String())
	}
	runID := decodeRunID(t, started.Body.Bytes())
	for attempt := 0; attempt < 2; attempt++ {
		cancelled := test.request(http.MethodDelete, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+runID, nil, token)
		if cancelled.Code != http.StatusOK {
			t.Fatalf("cancel attempt %d status = %d; body: %s", attempt+1, cancelled.Code, cancelled.Body.String())
		}
		var run analysisRunResponse
		if err := json.Unmarshal(cancelled.Body.Bytes(), &run); err != nil {
			t.Fatalf("decode cancel attempt %d: %v", attempt+1, err)
		}
		if run.ID != runID || run.InvestigationID != investigationID || run.Status != "cancelled" {
			t.Errorf("cancel attempt %d run = %#v", attempt+1, run)
		}
	}

	polled := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+runID, nil, token)
	if polled.Code != http.StatusOK || polled.Body.String() == "" {
		t.Fatalf("poll cancelled run = %d %s", polled.Code, polled.Body.String())
	}
	var run analysisRunResponse
	if err := json.Unmarshal(polled.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode cancelled poll: %v", err)
	}
	if run.Status != "cancelled" {
		t.Errorf("cancelled poll = %#v", run)
	}

	worker := <-queuedWorker
	worker()
	if captureCalls.Load() != 0 {
		t.Errorf("cancelled queued run called provider %d times", captureCalls.Load())
	}
	assertNoPublishedAnalysis(t)
	var investigation store.Investigation
	if err := model.DB.First(&investigation, "id = ?", investigationID).Error; err != nil {
		t.Fatalf("reload cancelled investigation: %v", err)
	}
	if investigation.Status != store.InvestigationPending || investigation.CurrentResultID != nil || investigation.TargetLocked {
		t.Errorf("cancelled first run left investigation = %#v", investigation)
	}
}

func TestCancellationReachesCutoffProviderTraversalAndEvaluator(t *testing.T) {
	for _, phase := range []string{"cutoff", "provider", "evaluator"} {
		t.Run(phase, func(t *testing.T) {
			test := newAuthHTTPTest(t)
			migrateAnalysisTestTables(t)
			cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
			provider := successfulProvider(cutoff)
			evaluator := lifecycleEvaluator{evaluate: func(context.Context, analysis.EvaluationInput) (analysis.Evaluation, error) {
				return successfulEvaluator().result, nil
			}}
			entered := make(chan struct{})
			cancelled := make(chan struct{})
			block := func(ctx context.Context) error {
				close(entered)
				<-ctx.Done()
				close(cancelled)
				return ctx.Err()
			}
			switch phase {
			case "cutoff":
				provider.capture = func(ctx context.Context, _ string) (analysis.BlockCutoff, error) {
					return analysis.BlockCutoff{}, block(ctx)
				}
			case "provider":
				provider.fetch = func(ctx context.Context, _ analysis.AddressTransferPageRequest) (analysis.AddressTransferPage, error) {
					return analysis.AddressTransferPage{}, block(ctx)
				}
			case "evaluator":
				evaluator.evaluate = func(ctx context.Context, _ analysis.EvaluationInput) (analysis.Evaluation, error) {
					return analysis.Evaluation{}, block(ctx)
				}
			}

			test.engine = gin.New()
			apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{Provider: provider, Evaluator: evaluator})
			token := ownerToken(t, test.owner.ID)
			investigationID := createTargetedInvestigation(t, test, token)
			started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
			if started.Code != http.StatusAccepted {
				t.Fatalf("start at %s status = %d; body: %s", phase, started.Code, started.Body.String())
			}
			runID := decodeRunID(t, started.Body.Bytes())
			awaitSignal(t, entered, phase+" was not entered")

			response := test.request(http.MethodDelete, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+runID, nil, token)
			if response.Code != http.StatusOK {
				t.Fatalf("cancel at %s status = %d; body: %s", phase, response.Code, response.Body.String())
			}
			var run analysisRunResponse
			if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
				t.Fatalf("decode %s cancellation: %v", phase, err)
			}
			if run.Status != "cancelled" {
				t.Errorf("cancel at %s run = %#v", phase, run)
			}
			awaitSignal(t, cancelled, phase+" did not observe cancellation")
			assertNoPublishedAnalysis(t)
			assertStableInvestigation(t, investigationID, store.InvestigationPending, "")
		})
	}
}

func TestLostRunRestoresStableStateAllowsRetryAndFencesTheStaleWorker(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	oldWorker := make(chan func(), 1)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  successfulProvider(cutoff),
		Evaluator: successfulEvaluator(),
		LaunchWorker: func(worker func()) {
			oldWorker <- worker
		},
	})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)
	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if started.Code != http.StatusAccepted {
		t.Fatalf("start lost run status = %d; body: %s", started.Code, started.Body.String())
	}
	lostRunID := decodeRunID(t, started.Body.Bytes())
	assertStableInvestigation(t, investigationID, store.InvestigationAnalyzing, "")

	retryWorker := make(chan func(), 1)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  successfulProvider(cutoff.Add(time.Hour)),
		Evaluator: successfulEvaluator(),
		LaunchWorker: func(worker func()) {
			retryWorker <- worker
		},
	})
	other := store.User{
		Username: "lost-other-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Email:    "lost-other-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@auth-test.invalid",
		Password: "test-only",
		Salt:     "test-only",
	}
	if err := model.DB.Create(&other).Error; err != nil {
		t.Fatalf("create run-lost other owner: %v", err)
	}
	otherToken := ownerToken(t, other.ID)
	crossOwner := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+lostRunID, nil, otherToken)
	missingInvestigation := test.request(http.MethodGet, "/api/v1/investigations/missingInvestigation00000/analysis-runs/"+lostRunID, nil, otherToken)
	if crossOwner.Code != http.StatusNotFound || crossOwner.Body.String() != missingInvestigation.Body.String() || !responseHasCode(crossOwner.Body.Bytes(), "investigation_not_found") {
		t.Fatalf("cross-owner lost poll = %d %s; missing = %d %s", crossOwner.Code, crossOwner.Body.String(), missingInvestigation.Code, missingInvestigation.Body.String())
	}
	assertStableInvestigation(t, investigationID, store.InvestigationAnalyzing, "")

	lost := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+lostRunID, nil, token)
	if lost.Code != http.StatusNotFound || !responseHasCode(lost.Body.Bytes(), "run_lost") {
		t.Fatalf("owned lost poll = %d %s", lost.Code, lost.Body.String())
	}
	assertStableInvestigation(t, investigationID, store.InvestigationPending, "")

	retried := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if retried.Code != http.StatusAccepted {
		t.Fatalf("retry start status = %d; body: %s", retried.Code, retried.Body.String())
	}
	retryRunID := decodeRunID(t, retried.Body.Bytes())
	(<-retryWorker)()
	retryResult := getRun(t, test, token, investigationID, retryRunID)
	if retryResult.Status != "completed" || retryResult.ResultID == nil {
		t.Fatalf("retry result = %#v", retryResult)
	}
	currentResultID := *retryResult.ResultID

	(<-oldWorker)()
	assertStableInvestigation(t, investigationID, store.InvestigationCompleted, currentResultID)
	var datasets int64
	if err := model.DB.Model(&store.AnalysisDataset{}).Where("investigation_id = ?", investigationID).Count(&datasets).Error; err != nil {
		t.Fatalf("count datasets after stale worker: %v", err)
	}
	if datasets != 1 {
		t.Errorf("datasets after stale worker = %d, want 1", datasets)
	}
}

func TestReanalysisPreservesPreviousResultUntilSuccessfulCutover(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	firstWorker := make(chan func(), 1)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  successfulProvider(cutoff),
		Evaluator: successfulEvaluator(),
		LaunchWorker: func(worker func()) {
			firstWorker <- worker
		},
	})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)
	firstStart := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	firstRunID := decodeRunID(t, firstStart.Body.Bytes())
	(<-firstWorker)()
	first := getRun(t, test, token, investigationID, firstRunID)
	if first.Status != "completed" || first.ResultID == nil {
		t.Fatalf("first stable run = %#v", first)
	}
	stableResultID := *first.ResultID

	evaluatorEntered := make(chan struct{})
	evaluatorCancelled := make(chan struct{})
	blockedEvaluator := lifecycleEvaluator{evaluate: func(ctx context.Context, _ analysis.EvaluationInput) (analysis.Evaluation, error) {
		close(evaluatorEntered)
		<-ctx.Done()
		close(evaluatorCancelled)
		return analysis.Evaluation{}, ctx.Err()
	}}
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  successfulProvider(cutoff.Add(time.Hour)),
		Evaluator: blockedEvaluator,
	})
	secondStart := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if secondStart.Code != http.StatusAccepted {
		t.Fatalf("reanalysis start status = %d; body: %s", secondStart.Code, secondStart.Body.String())
	}
	secondRunID := decodeRunID(t, secondStart.Body.Bytes())
	awaitSignal(t, evaluatorEntered, "reanalysis evaluator was not entered")
	assertStableInvestigation(t, investigationID, store.InvestigationAnalyzing, stableResultID)
	if got := currentResultID(t, test, token, investigationID); got != stableResultID {
		t.Errorf("current result during reanalysis = %q, want %q", got, stableResultID)
	}

	cancelled := test.request(http.MethodDelete, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+secondRunID, nil, token)
	if cancelled.Code != http.StatusOK || !responseHasRunStatus(cancelled.Body.Bytes(), "cancelled") {
		t.Fatalf("cancel reanalysis = %d %s", cancelled.Code, cancelled.Body.String())
	}
	awaitSignal(t, evaluatorCancelled, "reanalysis evaluator did not observe cancellation")
	assertStableInvestigation(t, investigationID, store.InvestigationCompleted, stableResultID)
	if got := currentResultID(t, test, token, investigationID); got != stableResultID {
		t.Errorf("current result after cancellation = %q, want %q", got, stableResultID)
	}

	retryWorker := make(chan func(), 1)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  successfulProvider(cutoff.Add(2 * time.Hour)),
		Evaluator: successfulEvaluator(),
		LaunchWorker: func(worker func()) {
			retryWorker <- worker
		},
	})
	retryStart := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if retryStart.Code != http.StatusAccepted {
		t.Fatalf("reanalysis retry status = %d; body: %s", retryStart.Code, retryStart.Body.String())
	}
	retryRunID := decodeRunID(t, retryStart.Body.Bytes())
	if got := currentResultID(t, test, token, investigationID); got != stableResultID {
		t.Errorf("current result before retry publication = %q, want %q", got, stableResultID)
	}
	(<-retryWorker)()
	retry := getRun(t, test, token, investigationID, retryRunID)
	if retry.Status != "completed" || retry.ResultID == nil || *retry.ResultID == stableResultID {
		t.Fatalf("completed reanalysis retry = %#v; previous = %q", retry, stableResultID)
	}
	assertStableInvestigation(t, investigationID, store.InvestigationCompleted, *retry.ResultID)
	if got := currentResultID(t, test, token, investigationID); got != *retry.ResultID {
		t.Errorf("current result after cutover = %q, want %q", got, *retry.ResultID)
	}
	var datasets int64
	if err := model.DB.Model(&store.AnalysisDataset{}).Where("investigation_id = ?", investigationID).Count(&datasets).Error; err != nil {
		t.Fatalf("count reanalysis datasets: %v", err)
	}
	if datasets != 2 {
		t.Errorf("reanalysis dataset count = %d, want 2", datasets)
	}

	lostStart := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if lostStart.Code != http.StatusAccepted {
		t.Fatalf("lost reanalysis start status = %d; body: %s", lostStart.Code, lostStart.Body.String())
	}
	lostRunID := decodeRunID(t, lostStart.Body.Bytes())
	lostWorker := <-retryWorker
	assertStableInvestigation(t, investigationID, store.InvestigationAnalyzing, *retry.ResultID)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  successfulProvider(cutoff.Add(3 * time.Hour)),
		Evaluator: successfulEvaluator(),
	})
	lost := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+lostRunID, nil, token)
	if lost.Code != http.StatusNotFound || !responseHasCode(lost.Body.Bytes(), "run_lost") {
		t.Fatalf("lost reanalysis poll = %d %s", lost.Code, lost.Body.String())
	}
	assertStableInvestigation(t, investigationID, store.InvestigationCompleted, *retry.ResultID)
	if got := currentResultID(t, test, token, investigationID); got != *retry.ResultID {
		t.Errorf("current result after lost reanalysis = %q, want %q", got, *retry.ResultID)
	}
	lostWorker()
	if err := model.DB.Model(&store.AnalysisDataset{}).Where("investigation_id = ?", investigationID).Count(&datasets).Error; err != nil {
		t.Fatalf("count datasets after lost reanalysis worker: %v", err)
	}
	if datasets != 2 {
		t.Errorf("datasets after lost reanalysis worker = %d, want 2", datasets)
	}
}

func TestCancellationAndPublicationHaveOneTerminalWinner(t *testing.T) {
	t.Run("cancellation wins", func(t *testing.T) {
		test := newAuthHTTPTest(t)
		migrateAnalysisTestTables(t)
		cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
		evaluatorEntered := make(chan struct{})
		releaseEvaluator := make(chan struct{})
		evaluator := lifecycleEvaluator{evaluate: func(ctx context.Context, _ analysis.EvaluationInput) (analysis.Evaluation, error) {
			close(evaluatorEntered)
			<-releaseEvaluator
			if ctx.Err() == nil {
				t.Error("evaluator context was not cancelled")
			}
			return successfulEvaluator().result, nil
		}}
		workerQueue := make(chan func(), 1)
		test.engine = gin.New()
		apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
			Provider:  successfulProvider(cutoff),
			Evaluator: evaluator,
			LaunchWorker: func(worker func()) {
				workerQueue <- worker
			},
		})
		token := ownerToken(t, test.owner.ID)
		investigationID := createTargetedInvestigation(t, test, token)
		started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
		runID := decodeRunID(t, started.Body.Bytes())
		workerDone := make(chan struct{})
		go func() {
			(<-workerQueue)()
			close(workerDone)
		}()
		awaitSignal(t, evaluatorEntered, "race evaluator was not entered")

		cancelled := test.request(http.MethodDelete, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+runID, nil, token)
		if cancelled.Code != http.StatusOK || !responseHasRunStatus(cancelled.Body.Bytes(), "cancelled") {
			t.Fatalf("cancellation winner response = %d %s", cancelled.Code, cancelled.Body.String())
		}
		close(releaseEvaluator)
		awaitSignal(t, workerDone, "cancelled race worker did not return")
		if run := getRun(t, test, token, investigationID, runID); run.Status != "cancelled" || run.ResultID != nil {
			t.Errorf("cancel winner terminal run = %#v", run)
		}
		assertNoPublishedAnalysis(t)
		assertStableInvestigation(t, investigationID, store.InvestigationPending, "")
	})

	t.Run("publication wins", func(t *testing.T) {
		test := newAuthHTTPTest(t)
		migrateAnalysisTestTables(t)
		cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
		workerQueue := make(chan func(), 1)
		test.engine = gin.New()
		apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
			Provider:  successfulProvider(cutoff),
			Evaluator: successfulEvaluator(),
			LaunchWorker: func(worker func()) {
				workerQueue <- worker
			},
		})
		token := ownerToken(t, test.owner.ID)
		investigationID := createTargetedInvestigation(t, test, token)
		started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
		runID := decodeRunID(t, started.Body.Bytes())

		publicationEntered := make(chan struct{})
		releasePublication := make(chan struct{})
		var armed atomic.Bool
		var blockOnce sync.Once
		callbackName := "test:block-analysis-publication"
		if err := model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
			if !armed.Load() || tx.Statement.Table != "investigations" {
				return
			}
			blockOnce.Do(func() {
				close(publicationEntered)
				<-releasePublication
			})
		}); err != nil {
			t.Fatalf("register publication callback: %v", err)
		}
		t.Cleanup(func() { _ = model.DB.Callback().Update().Remove(callbackName) })
		armed.Store(true)
		workerDone := make(chan struct{})
		go func() {
			(<-workerQueue)()
			close(workerDone)
		}()
		awaitSignal(t, publicationEntered, "publication transaction was not entered")

		cancelStarted := make(chan struct{})
		cancelResponse := make(chan []byte, 1)
		cancelStatus := make(chan int, 1)
		go func() {
			close(cancelStarted)
			response := test.request(http.MethodDelete, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+runID, nil, token)
			cancelStatus <- response.Code
			cancelResponse <- append([]byte(nil), response.Body.Bytes()...)
		}()
		awaitSignal(t, cancelStarted, "concurrent cancellation did not start")
		close(releasePublication)
		awaitSignal(t, workerDone, "publication winner worker did not return")
		status := <-cancelStatus
		body := <-cancelResponse
		if status != http.StatusOK || !responseHasRunStatus(body, "completed") {
			t.Fatalf("publication winner cancel response = %d %s", status, body)
		}
		run := getRun(t, test, token, investigationID, runID)
		if run.Status != "completed" || run.ResultID == nil {
			t.Fatalf("publication winner terminal run = %#v", run)
		}
		if got := currentResultID(t, test, token, investigationID); got != *run.ResultID {
			t.Errorf("publication winner current result = %q, want %q", got, *run.ResultID)
		}
		repeated := test.request(http.MethodDelete, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+runID, nil, token)
		if repeated.Code != http.StatusOK || !responseHasRunStatus(repeated.Body.Bytes(), "completed") {
			t.Errorf("repeat cancel after publication = %d %s", repeated.Code, repeated.Body.String())
		}
	})
}

func TestPollingStaysResponsiveWhilePublicationCommits(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	workerQueue := make(chan func(), 1)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:     successfulProvider(cutoff),
		Evaluator:    successfulEvaluator(),
		LaunchWorker: func(worker func()) { workerQueue <- worker },
	})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)
	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	runID := decodeRunID(t, started.Body.Bytes())

	publicationEntered := make(chan struct{})
	releasePublication := make(chan struct{})
	var armed atomic.Bool
	var blockOnce sync.Once
	callbackName := "test:block-analysis-publication-poll"
	if err := model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if !armed.Load() || tx.Statement.Table != "investigations" {
			return
		}
		blockOnce.Do(func() {
			close(publicationEntered)
			<-releasePublication
		})
	}); err != nil {
		t.Fatalf("register publication callback: %v", err)
	}
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove(callbackName) })
	armed.Store(true)
	workerDone := make(chan struct{})
	go func() {
		(<-workerQueue)()
		close(workerDone)
	}()
	awaitSignal(t, publicationEntered, "publication transaction was not entered")

	// The publication must not hold the run registry while it commits, or the
	// frontend's poll loop stalls for the whole write.
	polled := make(chan analysisRunResponse, 1)
	go func() { polled <- getRun(t, test, token, investigationID, runID) }()
	select {
	case run := <-polled:
		if run.Status != "running" {
			t.Errorf("poll during publication = %#v, want a running run", run)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("polling blocked while the publication transaction was in flight")
	}

	close(releasePublication)
	awaitSignal(t, workerDone, "publication worker did not return")
	if run := getRun(t, test, token, investigationID, runID); run.Status != "completed" || run.ResultID == nil {
		t.Errorf("terminal run after publication = %#v", run)
	}
}

func TestDeletingActiveInvestigationCancelsWorkerAndLeavesNoOrphanDataset(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	cutoff := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	providerEntered := make(chan struct{})
	providerCancelled := make(chan struct{})
	releaseProvider := make(chan struct{})
	provider := successfulProvider(cutoff)
	provider.fetch = func(ctx context.Context, request analysis.AddressTransferPageRequest) (analysis.AddressTransferPage, error) {
		if request.Address != testTargetAddress {
			return analysis.AddressTransferPage{}, nil
		}
		close(providerEntered)
		<-ctx.Done()
		close(providerCancelled)
		<-releaseProvider
		return analysis.AddressTransferPage{Transactions: oneTransferCollection(cutoff).Transactions}, nil
	}
	workerQueue := make(chan func(), 1)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  provider,
		Evaluator: successfulEvaluator(),
		LaunchWorker: func(worker func()) {
			workerQueue <- worker
		},
	})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)
	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	if started.Code != http.StatusAccepted {
		t.Fatalf("active-delete start status = %d; body: %s", started.Code, started.Body.String())
	}
	runID := decodeRunID(t, started.Body.Bytes())
	workerDone := make(chan struct{})
	go func() {
		(<-workerQueue)()
		close(workerDone)
	}()
	awaitSignal(t, providerEntered, "active-delete provider was not entered")

	deleted := test.request(http.MethodDelete, "/api/v1/investigations/"+investigationID, nil, token)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("active delete status = %d; body: %s", deleted.Code, deleted.Body.String())
	}
	awaitSignal(t, providerCancelled, "active-delete provider did not observe cancellation")
	detail := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID, nil, token)
	if detail.Code != http.StatusNotFound || !responseHasCode(detail.Body.Bytes(), "investigation_not_found") {
		t.Errorf("deleted active investigation detail = %d %s", detail.Code, detail.Body.String())
	}
	poll := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+runID, nil, token)
	if poll.Code != http.StatusNotFound || !responseHasCode(poll.Body.Bytes(), "investigation_not_found") {
		t.Errorf("deleted active investigation poll = %d %s", poll.Code, poll.Body.String())
	}

	close(releaseProvider)
	awaitSignal(t, workerDone, "late active-delete worker did not return")
	assertNoPublishedAnalysis(t)
	var investigations int64
	if err := model.DB.Model(&store.Investigation{}).Where("id = ?", investigationID).Count(&investigations).Error; err != nil {
		t.Fatalf("count active-deleted investigation: %v", err)
	}
	if investigations != 0 {
		t.Errorf("active-deleted investigation count = %d, want 0", investigations)
	}
}

func TestAnalysisRunCancellationIsOwnerScoped(t *testing.T) {
	test := newAuthHTTPTest(t)
	migrateAnalysisTestTables(t)
	workerQueue := make(chan func(), 1)
	test.engine = gin.New()
	apiRouter.ApiRouterWithAnalysisOptions(test.engine, analysis.Options{
		Provider:  successfulProvider(time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)),
		Evaluator: successfulEvaluator(),
		LaunchWorker: func(worker func()) {
			workerQueue <- worker
		},
	})
	token := ownerToken(t, test.owner.ID)
	investigationID := createTargetedInvestigation(t, test, token)
	started := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/analysis-runs", nil, token)
	runID := decodeRunID(t, started.Body.Bytes())
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	other := store.User{Username: "cancel-other-" + suffix, Email: "cancel-other-" + suffix + "@auth-test.invalid", Password: "test-only", Salt: "test-only"}
	if err := model.DB.Create(&other).Error; err != nil {
		t.Fatalf("create cancellation other owner: %v", err)
	}
	otherToken := ownerToken(t, other.ID)

	crossOwner := test.request(http.MethodDelete, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+runID, nil, otherToken)
	missing := test.request(http.MethodDelete, "/api/v1/investigations/missingInvestigation00000/analysis-runs/"+runID, nil, otherToken)
	if crossOwner.Code != http.StatusNotFound || crossOwner.Body.String() != missing.Body.String() || !responseHasCode(crossOwner.Body.Bytes(), "investigation_not_found") {
		t.Fatalf("cross-owner cancel = %d %s; missing = %d %s", crossOwner.Code, crossOwner.Body.String(), missing.Code, missing.Body.String())
	}
	if run := getRun(t, test, token, investigationID, runID); run.Status != "queued" {
		t.Errorf("cross-owner cancellation changed run = %#v", run)
	}
	owned := test.request(http.MethodDelete, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+runID, nil, token)
	if owned.Code != http.StatusOK || !responseHasRunStatus(owned.Body.Bytes(), "cancelled") {
		t.Fatalf("owner cancellation = %d %s", owned.Code, owned.Body.String())
	}
	(<-workerQueue)()
	assertNoPublishedAnalysis(t)
}

func getRun(t *testing.T, test *authHTTPTest, token, investigationID, runID string) analysisRunResponse {
	t.Helper()
	response := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/analysis-runs/"+runID, nil, token)
	if response.Code != http.StatusOK {
		t.Fatalf("get run status = %d; body: %s", response.Code, response.Body.String())
	}
	var run analysisRunResponse
	if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode run response: %v", err)
	}
	return run
}

func responseHasCode(body []byte, code string) bool {
	var response struct {
		Code string `json:"code"`
	}
	return json.Unmarshal(body, &response) == nil && response.Code == code
}

func responseHasRunStatus(body []byte, status string) bool {
	var response struct {
		Status string `json:"status"`
	}
	return json.Unmarshal(body, &response) == nil && response.Status == status
}

func currentResultID(t *testing.T, test *authHTTPTest, token, investigationID string) string {
	t.Helper()
	response := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/current-result", nil, token)
	if response.Code != http.StatusOK {
		t.Fatalf("current result status = %d; body: %s", response.Code, response.Body.String())
	}
	var result struct {
		Dataset struct {
			ID string `json:"id"`
		} `json:"dataset"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode current result: %v", err)
	}
	return result.Dataset.ID
}

func awaitSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal(failure)
	}
}

func assertStableInvestigation(t *testing.T, investigationID string, status store.InvestigationStatus, resultID string) {
	t.Helper()
	var investigation store.Investigation
	if err := model.DB.First(&investigation, "id = ?", investigationID).Error; err != nil {
		t.Fatalf("reload stable investigation: %v", err)
	}
	if investigation.Status != status {
		t.Errorf("stable investigation status = %q, want %q", investigation.Status, status)
	}
	if resultID == "" {
		if investigation.CurrentResultID != nil {
			t.Errorf("stable investigation result = %q, want nil", *investigation.CurrentResultID)
		}
	} else if investigation.CurrentResultID == nil || *investigation.CurrentResultID != resultID {
		t.Errorf("stable investigation result = %v, want %q", investigation.CurrentResultID, resultID)
	}
}
