package flow

import (
	"strings"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

func TestConfigurationRequiresExplicitDestinationsAndVerification(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Fatal("empty configuration must not enable execution")
	}
	if safeURL("http://public.example/v1") {
		t.Fatal("cleartext remote endpoint accepted")
	}
	if !safeURL("https://model.internal/v1") {
		t.Fatal("private HTTPS endpoint rejected")
	}
}

func TestRepositoryEngineAcceptsOnlyPinnedLocalNativeProfile(t *testing.T) {
	c := Config{ProjectID: "project", PublicURL: "http://localhost:8090", SigningKeyFile: "/secrets/signing", ArtifactTokenFile: "/secrets/artifacts", ArtifactDirectory: "/artifacts", PolicyVersion: "v1", MaxTokens: 20000, BudgetMinutes: 5,
		AllowedPaths: []string{"app"}, Checks: []workresult.Check{{ID: "check", Directory: ".", Command: []string{"go", "test", "./..."}}}}
	c.Model = workerapi.Model{Endpoint: "https://openrouter.ai/api/v1", Name: "fixture", MaxOutputTokens: 2000, Currency: "USD", InputMicrosPerMillion: 100, OutputMicrosPerMillion: 100}
	c.Git = github.Config{APIURL: "https://api.github.com", CloneURL: "https://github.com/owner/repo.git", Repository: "owner/repo", AuthorName: "Test", AuthorEmail: "test@example.com", BaseBranch: "main"}
	c.RepositoryEngine = &runtimeapi.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-codex", Template: "kint-one-task", Revision: strings.Repeat("a", 64)}
	c.RepositoryEngineGatewayURL = "https://marina.marina-system.svc"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	changed := c
	selection := *c.RepositoryEngine
	selection.Endpoint = "arbitrary.example:8083"
	changed.RepositoryEngine = &selection
	if err := changed.Validate(); err == nil {
		t.Fatal("untrusted cleartext controller endpoint accepted")
	}
	selection = *c.RepositoryEngine
	selection.Template = "../shared"
	changed.RepositoryEngine = &selection
	if err := changed.Validate(); err == nil {
		t.Fatal("invalid attempt Secret name accepted")
	}
}

func TestAnalysisConfigurationDoesNotRequireRepositorySettings(t *testing.T) {
	c := Config{ProjectID: "project", PublicURL: "http://localhost:8090", SigningKeyFile: "/secrets/signing", ArtifactTokenFile: "/secrets/artifacts", ArtifactDirectory: "/artifacts", PolicyVersion: "v1", MaxTokens: 20000, BudgetMinutes: 5}
	c.Model = workerapi.Model{Endpoint: "https://openrouter.ai/api/v1", Name: "fixture", MaxOutputTokens: 2000, Currency: "USD", InputMicrosPerMillion: 100, OutputMicrosPerMillion: 100}
	c.Analysis = &analysis.Profile{Version: 1, Revision: 1, Image: "analysis@sha256:" + strings.Repeat("a", 64), HarnessVersion: analysis.Harness, MaxTokens: 20000, BudgetMinutes: 5, Fixture: analysis.DiagnosticFixture()}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	authority := c.AnalysisAuthority()
	c.Model.Endpoint = "https://different.example/v1"
	if c.AnalysisAuthority() == authority {
		t.Fatal("model destination change did not invalidate task authorization")
	}
	c.Analysis.Version = 2
	if c.Validate() == nil {
		t.Fatal("unsupported profile version accepted")
	}
}
