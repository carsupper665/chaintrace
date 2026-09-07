package analysis

import (
	"chaintrace/utils"
	"context"
	"errors"
	"time"
)

const (
	// LearnedScoreTransferCap must equal agent/scoring/model/manifest.json's
	// transferCap (checked by learned_score_manifest_test.go). It bounds the
	// Target's own transfer history the learned scorer sees, independent of
	// whatever Analysis Scope the Owner picked for the rules-based graph
	// traversal (ADR-0015: "Scoring reads the Target's own transfer history
	// at the model's cap, not the Analysis Dataset").
	LearnedScoreTransferCap = 10000
	// learnedScoreTimeout bounds fetching up to LearnedScoreTransferCap
	// transfers (paginated) plus one HTTP call to the scorer, as a single
	// sub-operation well under analysisRunTimeout (30m). A busy address can
	// legitimately time out here before reaching the cap; that just means no
	// learned score for this run, not a failed run.
	learnedScoreTimeout = 3 * time.Minute
)

// LearnedScoreInput is the Target's own transfer history, independent of any
// graph traversal, plus the window it was collected under.
type LearnedScoreInput struct {
	TargetAddress string
	Network       string
	Asset         string
	WindowStart   time.Time
	WindowEnd     time.Time
	Transfers     []TRC20Transfer
	Truncated     bool
	Decimals      int
}

// LearnedScoreResult is the unsupervised model's anomaly score. Source
// identifies which trained artifact produced it (the model manifest's
// trainingDataHash), for later comparison work (Phase 4).
type LearnedScoreResult struct {
	Score  float64
	Source string
}

// LearnedScorer is a second, independent risk measurement alongside
// RiskEvaluator. Unlike RiskEvaluator, an error here never fails the run: see
// RunManager.attemptLearnedScore.
type LearnedScorer interface {
	Score(context.Context, LearnedScoreInput) (LearnedScoreResult, error)
}

// attemptLearnedScore tries to compute a learned score for the run's target
// and never returns an error: any failure (fetch, scorer call, timeout) is
// logged and swallowed, leaving the run to complete rules-only. This is what
// makes "a scorer returning 5xx, or timing out, leaves the run completed with
// rules only" true structurally rather than by convention.
func (m *RunManager) attemptLearnedScore(ctx context.Context, request CollectionRequest) (*float64, string) {
	if m.learnedScorer == nil {
		return nil, ""
	}
	scoreCtx, cancel := context.WithTimeout(ctx, learnedScoreTimeout)
	defer cancel()

	transfers, truncated, err := fetchTargetOwnTransfers(scoreCtx, m.provider, request, LearnedScoreTransferCap)
	if err != nil {
		logLearnedScoreFailure("fetch", err)
		return nil, ""
	}
	result, err := m.learnedScorer.Score(scoreCtx, LearnedScoreInput{
		TargetAddress: request.TargetAddress,
		Network:       request.Network,
		Asset:         request.Asset,
		WindowStart:   request.WindowStart,
		WindowEnd:     request.WindowEnd,
		Transfers:     transfers,
		Truncated:     truncated,
		Decimals:      6, // TRC20 USDT is fixed at 6 decimals (analysis/collector.go filters on the same literal)
	})
	if err != nil {
		logLearnedScoreFailure("score", err)
		return nil, ""
	}
	score := result.Score
	return &score, result.Source
}

// fetchTargetOwnTransfers pages through exactly one address's own transfers,
// independent of Collector.Collect's shared MaximumTransferLimit and graph
// traversal (the learned model scores the Target's own history alone, never
// a neighbour, per ADR-0015). It reuses Collect's eligibility filter and
// dedup (eligiblePageTransfers, transferKey) so what counts as an eligible
// transfer stays defined in one place.
//
// TransferLimit passed to the provider is MaximumTransferLimit, a legal
// per-page size cap (analysis/trongrid.go rejects anything larger) — it does
// not bound the total collected here, which this loop enforces itself
// against cap.
func fetchTargetOwnTransfers(
	ctx context.Context, provider ChainDataProvider, request CollectionRequest, cap int,
) (transfers []TRC20Transfer, truncated bool, err error) {
	if provider == nil {
		return nil, false, errors.New("chain data provider is not configured")
	}
	seenTransfers := make(map[string]struct{})
	transfers = make([]TRC20Transfer, 0, cap)
	cursor := ""
	seenCursors := map[string]struct{}{"": {}}
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		page, err := provider.FetchAddressTransfers(ctx, AddressTransferPageRequest{
			Address:        request.TargetAddress,
			Network:        request.Network,
			Asset:          request.Asset,
			WindowStart:    request.WindowStart,
			WindowEnd:      request.WindowEnd,
			CutoffBlockID:  request.CutoffBlockID,
			TransferLimit:  MaximumTransferLimit,
			TraversalDepth: 1,
			Cursor:         cursor,
		})
		if err != nil {
			return nil, false, err
		}
		for _, candidate := range eligiblePageTransfers(page, request, request.TargetAddress) {
			key := transferKey(candidate.transaction, candidate.transfer)
			if _, exists := seenTransfers[key]; exists {
				continue
			}
			seenTransfers[key] = struct{}{}
			transfers = append(transfers, candidate.transfer)
			if len(transfers) == cap {
				return transfers, true, nil
			}
		}
		if page.NextCursor == "" {
			return transfers, false, nil
		}
		if _, repeated := seenCursors[page.NextCursor]; repeated {
			return nil, false, errors.New("provider repeated a page cursor")
		}
		seenCursors[page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}
}

func logLearnedScoreFailure(stage string, err error) {
	if utils.SysLog != nil {
		utils.SysLog.Warnf("learned score %s unavailable: %v", stage, err)
	}
}
