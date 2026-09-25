package analysisbyo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

func TestRunBindsOneDiagnosticReportWithoutValidatorAnswers(t *testing.T) {
	fixture := analysis.DiagnosticFixture()
	input := analysis.Input{Version: 1, ProfileDigest: strings.Repeat("a", 64), InputDigest: fixture.InputDigest(), Sources: fixture.Sources,
		Model: "fixture", ModelURL: gatewayModelURL, ToolURL: gatewayToolURL, Tools: []string{"read_context_source"}}
	raw, _ := json.Marshal(input)
	called := false
	result, err := run(context.Background(), raw, "inert-token", func(_ context.Context, got analysis.Input, token string) (analysis.Report, error) {
		called = true
		if got.InputDigest != fixture.InputDigest() || token != "inert-token" {
			t.Fatal("frozen input or scoped credential was lost")
		}
		return analysis.Report{Summary: "Blocked runs", Findings: fixture.Expected}, nil
	})
	if err != nil || !called {
		t.Fatalf("run failed: %v", err)
	}
	encoded, _ := json.Marshal(result)
	if _, err := workresult.Parse(encoded, workresult.Expected{Stage: "analyze", InputDigest: input.InputDigest, ProfileDigest: input.ProfileDigest, Now: time.Now()}); err != nil {
		t.Fatalf("BYO result did not satisfy the shared boundary: %v", err)
	}
	input.InputDigest = strings.Repeat("b", 64)
	raw, _ = json.Marshal(input)
	if _, err := run(context.Background(), raw, "inert-token", func(context.Context, analysis.Input, string) (analysis.Report, error) {
		t.Fatal("invalid input reached LangChain")
		return analysis.Report{}, nil
	}); err == nil {
		t.Fatal("mismatched fixture digest accepted")
	}
}
