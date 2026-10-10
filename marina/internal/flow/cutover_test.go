package flow

import (
	"strings"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/kagent"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

func TestRepositoryAttemptCannotFallBackToAgentJob(t *testing.T) {
	c := Controller{}
	for _, key := range []string{"execution:implement", "execution:analyze"} {
		if _, err := c.runtimeFor(runtimeapi.Specification{IdempotencyKey: key}); err == nil {
			t.Fatalf("legacy agent runtime selected for %s", key)
		}
	}
}

func TestRepositoryConfigurationRequiresNativeEngine(t *testing.T) {
	c := Config{ProjectID: "project", PublicURL: "http://localhost:8090", SigningKeyFile: "/secrets/signing", ArtifactTokenFile: "/secrets/artifacts", ArtifactDirectory: "/artifacts", PolicyVersion: "v1", MaxTokens: 20000, BudgetMinutes: 5,
		AllowedPaths: []string{"app"}, Checks: []workresult.Check{{ID: "check", Directory: ".", Command: []string{"go", "test", "./..."}}},
		Model: workerapi.Model{Endpoint: "https://openrouter.ai/api/v1", Name: "fixture", MaxOutputTokens: 2000, Currency: "USD"},
		Git:   github.Config{APIURL: "https://api.github.com", CloneURL: "https://github.com/owner/repo.git", Repository: "owner/repo", AuthorName: "Test", AuthorEmail: "test@example.com", BaseBranch: "main"}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "engine") {
		t.Fatalf("legacy configuration enabled execution: %v", err)
	}
	c.RepositoryEngine = &runtimeapi.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-codex"}
	c.RepositoryEngineGatewayURL = "https://marina.marina-system.svc"
	c.ReviewerModel = &workerapi.Model{Endpoint: "https://openrouter.ai/api/v1", Name: "reviewer", MaxOutputTokens: 2000, Currency: "USD"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
