package analysis_test

import (
	"chaintrace/analysis"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The learned scorer's window and cap are duplicated by necessity: Go collects
// the Target's own transfers independently of the Python model, and the two
// sides cannot share a compile-time constant. This test is the tripwire —
// retraining the model with a different window or cap must fail here loudly,
// not silently desync from what Go actually collects (see "Model staleness"
// in docs/learned-risk-scoring-plan.md).
func TestLearnedScoreConstantsMatchModelManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.FromSlash("../agent/scoring/model/manifest.json"))
	if err != nil {
		t.Fatalf("read model manifest: %v", err)
	}
	var manifest struct {
		WindowDays  int `json:"windowDays"`
		TransferCap int `json:"transferCap"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode model manifest: %v", err)
	}

	if got, want := analysis.LearnedScoreTransferCap, manifest.TransferCap; got != want {
		t.Errorf("LearnedScoreTransferCap = %d, model manifest transferCap = %d", got, want)
	}
	if got, want := int(analysis.CollectionWindow/(24*time.Hour)), manifest.WindowDays; got != want {
		t.Errorf("CollectionWindow = %d days, model manifest windowDays = %d", got, want)
	}
}
