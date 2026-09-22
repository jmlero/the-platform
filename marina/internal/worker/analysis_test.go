package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/analysis"
)

func TestAnalysisWorkerRunsWithoutGitOrCodingHarness(t *testing.T) {
	fixture := analysis.DiagnosticFixture()
	script := filepath.Join(t.TempDir(), "report.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' '{\"summary\":\"Synthetic report\",\"findings\":[]}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	input := Input{Stage: "analyze", Analysis: &analysis.Input{Version: 1, ProfileDigest: strings.Repeat("a", 64), InputDigest: fixture.InputDigest(), Sources: fixture.Sources, Model: "fixture", ModelURL: "http://localhost:8090/model/v1"}, Harness: []string{script}}
	root := filepath.Join(t.TempDir(), "analysis")
	result, err := Run(context.Background(), input, root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Analysis == nil || result.Analysis.Summary != "Synthetic report" || result.ProfileDigest != input.Analysis.ProfileDigest || result.InputDigest != fixture.InputDigest() || result.SourceRevision != "" || len(result.Changes) > 0 {
		t.Fatalf("result=%+v", result)
	}
	if _, err = os.Stat(filepath.Join(root, "repo")); !os.IsNotExist(err) {
		t.Fatal("analysis created a repository workspace")
	}
	input.Analysis.Sources[0].Content += " edited"
	if _, err = Run(context.Background(), input, t.TempDir()); err == nil {
		t.Fatal("modified inputs accepted")
	}
}

func TestAnalysisFailureReportsOnlyAllowlistedCategory(t *testing.T) {
	fixture := analysis.DiagnosticFixture()
	for _, marker := range []string{"model_unavailable", "secret-value"} {
		script := filepath.Join(t.TempDir(), "fail.sh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' 'private response secret-value MARINA_ANALYSIS_FAILURE="+marker+"'\nexit 1\n"), 0700); err != nil {
			t.Fatal(err)
		}
		_, err := Run(context.Background(), Input{Stage: "analyze", Analysis: &analysis.Input{Version: 1, ProfileDigest: strings.Repeat("a", 64), InputDigest: fixture.InputDigest(), Sources: fixture.Sources}, Harness: []string{script}}, t.TempDir())
		if err == nil || strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "private response") {
			t.Fatalf("unsafe diagnostic: %v", err)
		}
		if marker == "model_unavailable" && !strings.Contains(err.Error(), marker) {
			t.Fatalf("missing safe category: %v", err)
		}
	}
}
