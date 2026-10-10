package worker

import (
	"context"
	"strings"
	"testing"
)

func TestVerifierEntryRejectsAgentStagesBeforeStartingTools(t *testing.T) {
	for _, stage := range []string{"implement", "analyze", ""} {
		_, err := Run(context.Background(), Input{Stage: stage}, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "verification") {
			t.Fatalf("stage %q reached the retired agent path: %v", stage, err)
		}
	}
}
