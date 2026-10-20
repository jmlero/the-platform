package flow

import (
	"slices"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/conversation"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
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
	reviewer := c.Model
	reviewer.Name = "independent-fixture"
	c.ReviewerModel = &reviewer
	c.Git = github.Config{APIURL: "https://api.github.com", CloneURL: "https://github.com/owner/repo.git", Repository: "owner/repo", AuthorName: "Test", AuthorEmail: "test@example.com", BaseBranch: "main"}
	c.RepositoryEngine = &runtimeapi.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-codex"}
	c.RepositoryEngineGatewayURL = "https://marina.marina-system.svc"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.WorkloadProfiles()[0].MinAttempts != 3 {
		t.Fatal("repository invocation can omit its review budget")
	}
	legacy := c.WorkloadProfiles()[0]
	if legacy.ID != "repository/v1" || !slices.Equal(legacy.Authority, []string{"source.read", "source.write", "pull_request.create"}) {
		t.Fatalf("legacy repository grant changed: %+v", legacy)
	}
	c.Conversation = &conversation.Profile{Engine: runtimeapi.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-conversation-byo"}, MaxTokens: 10000, BudgetMinutes: 5,
		Delegation: &workcontrol.ConversationBudget{TimeMinutes: 5, MaxAttempts: 3, MaxTokens: 20000, MaxConcurrency: 1, Authority: legacy.Authority}}
	// A Turn proposes tasks and its client decides what to run (decision 0033).
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "conversation.delegation is retired") {
		t.Fatalf("retired chat delegation accepted: %v", err)
	}
	c.Conversation.Delegation = nil
	if err := c.Validate(); err != nil {
		t.Fatalf("conversation without delegation: %v", err)
	}
	c.Conversation = nil
	c.RequiredCI = []string{"ci"}
	c.Delivery = "fast_forward"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	landing := c.WorkloadProfiles()[0]
	if landing.ID != "repository/v2" || !slices.Equal(landing.Authority, []string{"source.read", "source.write", "source.fast_forward"}) || landing.Digest == legacy.Digest {
		t.Fatalf("landing profile was not separately authorized: %+v", landing)
	}
	c.RequiredCI = []string{"  "}
	if err := c.Validate(); err == nil {
		t.Fatal("blank required CI accepted for landing")
	}
	c.RequiredCI = []string{"ci", "ci"}
	if err := c.Validate(); err == nil {
		t.Fatal("duplicate required CI accepted for landing")
	}
	c.RequiredCI = []string{"ci"}
	c.Delivery = "unknown"
	if err := c.Validate(); err == nil {
		t.Fatal("unknown delivery mode accepted")
	}
	c.Delivery = "fast_forward"
	changed := c
	selection := *c.RepositoryEngine
	selection.Endpoint = "arbitrary.example:8083"
	changed.RepositoryEngine = &selection
	if err := changed.Validate(); err == nil {
		t.Fatal("untrusted cleartext controller endpoint accepted")
	}
	selection = *c.RepositoryEngine
	selection.Template, selection.Revision = "kint-one-task", strings.Repeat("a", 64)
	changed.RepositoryEngine = &selection
	if err := changed.Validate(); err == nil {
		t.Fatal("a fixed prepared template would serve more than one attempt")
	}
}

func TestReviewerRequiresAnIndependentModelAndPreservesItsProfileIdentity(t *testing.T) {
	model := workerapi.Model{Endpoint: "https://models.example/v1", Name: "producer", Currency: "USD", MaxOutputTokens: 100}
	for _, test := range []struct {
		name     string
		reviewer *workerapi.Model
	}{
		{"missing", nil},
		{"same model", &model},
		{"same normalized model", &workerapi.Model{Endpoint: model.Endpoint, Name: " PRODUCER ", Currency: "USD", MaxOutputTokens: 100}},
		{"cleartext destination", &workerapi.Model{Endpoint: "http://remote.example", Name: "reviewer", Currency: "USD", MaxOutputTokens: 100}},
		{"currency change", &workerapi.Model{Endpoint: model.Endpoint, Name: "reviewer", Currency: "EUR", MaxOutputTokens: 100}},
		{"unbounded output", &workerapi.Model{Endpoint: model.Endpoint, Name: "reviewer", Currency: "USD", MaxOutputTokens: 65537}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if (Config{Model: model, ReviewerModel: test.reviewer}).ValidateReviewer() == nil {
				t.Fatal("invalid independent reviewer admitted")
			}
		})
	}
	reviewer := model
	reviewer.Name = "reviewer"
	c := Config{Model: model, ReviewerModel: &reviewer}
	if err := c.ValidateReviewer(); err != nil {
		t.Fatal(err)
	}
	digest := c.Digest()
	reviewer.Name = "replacement"
	if c.Digest() == digest {
		t.Fatal("reviewer model changed without a new installed profile")
	}
}
