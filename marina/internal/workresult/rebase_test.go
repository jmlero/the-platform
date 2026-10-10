package workresult

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestConflictBlockerIsAllowedOnlyForPinnedRebaseWithoutChanges(t *testing.T) {
	result := Result{Stage: "implement", SourceRevision: strings.Repeat("a", 40), Summary: "Conflict", ObservedAt: time.Now().UTC(), BlockingReason: "conflict in src/fix.go"}
	for _, scenario := range []string{"rebase", "ordinary implementation", "mixed changes"} {
		candidate := result
		if scenario == "mixed changes" {
			candidate.Changes = []Change{{Path: "src/fix.go", Mode: "100644", Content: []byte("unsafe")}}
		}
		data, _ := json.Marshal(candidate)
		_, err := Parse(data, Expected{Stage: "implement", SourceRevision: result.SourceRevision, AllowedPaths: []string{"src"}, AllowBlockReason: scenario != "ordinary implementation"})
		if (err == nil) != (scenario == "rebase") {
			t.Fatalf("%s: %v", scenario, err)
		}
	}
}
