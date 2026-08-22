package analysis_test

import (
	"chaintrace/analysis"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

const currentResultContractFixture = "../frontend/tests/fixtures/current-result-contract.json"

// The frontend hand-writes this payload's shape in TypeScript, where types are erased
// at runtime, so neither compiler notices when the two drift. The fixture is the one
// shape both sides assert against: renaming or adding a field here fails this test
// until the fixture and its TypeScript mirror move with it.
func TestCurrentResultMatchesPublishedContract(t *testing.T) {
	encoded, err := json.Marshal(sampleCurrentResult())
	if err != nil {
		t.Fatalf("marshal current result: %v", err)
	}
	fixture, err := os.ReadFile(filepath.FromSlash(currentResultContractFixture))
	if err != nil {
		t.Fatalf("read contract fixture: %v", err)
	}
	got, want := jsonKeyPaths(t, encoded), jsonKeyPaths(t, fixture)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("current-result keys drifted from the published contract\ngo       = %v\ncontract = %v", got, want)
	}
}

// Slices are populated so the contract covers the element shape too; a nil slice
// marshals to null and would hide every field inside it.
func sampleCurrentResult() analysis.CurrentResult {
	score := 0
	result := analysis.CurrentResult{}
	result.Assessment.Score = &score
	result.Assessment.Reasons = []string{""}
	result.Assessment.NodeAssessments = []analysis.NodeAssessment{{Reasons: []string{""}}}
	return result
}

func jsonKeyPaths(t *testing.T, encoded []byte) []string {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	paths := make([]string, 0)
	var walk func(prefix string, node any)
	walk = func(prefix string, node any) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				path := key
				if prefix != "" {
					path = prefix + "." + key
				}
				paths = append(paths, path)
				walk(path, child)
			}
		case []any:
			if len(value) > 0 {
				walk(prefix+"[]", value[0])
			}
		}
	}
	walk("", decoded)
	sort.Strings(paths)
	return paths
}
