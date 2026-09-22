package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
)

func TestWorkloadProfileExportsScopedGrantWithoutPrivateReferences(t *testing.T) {
	c := flow.Config{ProjectID: "project", PublicURL: "http://localhost:8090", SigningKeyFile: "/private/signing-key", ArtifactTokenFile: "/private/artifact-token", ArtifactDirectory: "/artifacts", PolicyVersion: "v1", MaxTokens: 20000, BudgetMinutes: 5, Model: workerapi.Model{Endpoint: "https://openrouter.ai/api/v1", Name: "fixture", TokenFile: "/private/model-token", MaxOutputTokens: 2000, Currency: "USD"}, Analysis: &analysis.Profile{Version: 1, Revision: 1, Image: "analysis@sha256:" + strings.Repeat("a", 64), HarnessVersion: analysis.Harness, MaxTokens: 20000, BudgetMinutes: 5, Fixture: analysis.DiagnosticFixture()}}
	path := filepath.Join(t.TempDir(), "workflow.json")
	b, _ := json.Marshal(c)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := run([]string{"workload-profile", "--config", path}, &out, &errout); code != 0 {
		t.Fatalf("code=%d: %s", code, errout.String())
	}
	if !strings.Contains(out.String(), c.AnalysisAuthority()) || strings.Contains(out.String(), "/private/") || strings.Contains(out.String(), "expected") {
		t.Fatalf("unsafe or missing profile summary: %s", out.String())
	}
}
