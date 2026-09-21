package flow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type repositoryProvider struct {
	Provider
	request workcontrol.RepositoryRequest
	result  workcontrol.RepositoryResult
	lose    bool
}

func (p *repositoryProvider) GetRepository(context.Context, string) (workcontrol.RepositoryRequest, error) {
	return p.request, nil
}
func (p *repositoryProvider) RecordRepositoryResult(_ context.Context, _ string, r workcontrol.RepositoryResult) error {
	p.result = r
	if p.lose {
		p.lose = false
		return errors.New("lost response")
	}
	p.request.Status = r.Status
	p.request.Binding = r.Binding
	return nil
}
func TestRepositoryAssociationRecoversLostConfirmationAndPinsExecution(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasSuffix(r.URL.Path, "/git/ref/heads/main") {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":{"sha":"` + strings.Repeat("a", 40) + `"}}`))
	}))
	defer server.Close()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")
	journal, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { journal.Close() }()
	config := Config{ProjectID: "p", Git: github.Config{APIURL: server.URL, CloneURL: server.URL + "/owner/first.git", Repository: "owner/first", BaseBranch: "main", TokenFile: "private-credential-reference"}}
	// Validation requires a readable token; the test endpoint needs no credential.
	config.Git.TokenFile = ""
	input := execution.ProjectBinding{ProjectID: "p", Provider: "deck", Repository: config.Git.CloneURL, Runtime: "test", PolicyVersion: "p1", ContextConfig: config.Digest(), ArtifactConfig: "a", DeliveryConfig: "github", BudgetMinutes: 30, MaxConcurrency: 1}
	binding, err := InitializeRepository(ctx, journal, config, input)
	if err != nil {
		t.Fatal(err)
	}
	provider := &repositoryProvider{request: workcontrol.RepositoryRequest{Revision: 1, Status: "pending", RepositoryRequestInput: workcontrol.RepositoryRequestInput{Repository: "owner/first", BaseBranch: "main"}}, lose: true}
	c := &Controller{Config: config, Journal: journal, Provider: provider}
	if err = c.ReconcileRepository(ctx); err == nil {
		t.Fatal("lost confirmation not injected")
	}
	journal.Close()
	journal, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	c.Journal = journal
	if err = c.ReconcileRepository(ctx); err != nil || calls != 1 {
		t.Fatalf("confirmation replay repeated validation: %d %v", calls, err)
	}
	provider.request = workcontrol.RepositoryRequest{Revision: 2, Status: "pending", RepositoryRequestInput: workcontrol.RepositoryRequestInput{ExpectedBindingVersion: binding.Version, Repository: "owner/second", BaseBranch: "main"}}
	if err = c.ReconcileRepository(ctx); err != nil {
		t.Fatal(err)
	}
	selected := provider.result.Binding
	if selected.Version <= binding.Version || selected.Repository != "owner/second" {
		t.Fatal("binding not revised")
	}
	// A restart retains the selected repository, while the old version is intact.
	restarted, err := InitializeRepository(ctx, journal, config, input)
	if err != nil || restarted.Version != selected.Version || restarted.Repository != selected.CloneURL {
		t.Fatal("restart reset association", err)
	}
	old, profile, err := journal.RepositorySnapshot(ctx, "p", binding.Version)
	if err != nil || old.Repository != "owner/first" || profile != config.Digest() {
		t.Fatal("active binding was overwritten", err)
	}
	if _, err = requestedRepository(config, workcontrol.RepositoryRequest{RepositoryRequestInput: workcontrol.RepositoryRequestInput{Repository: "https://evil.test/repo", BaseBranch: "main"}}); err == nil {
		t.Fatal("request could redirect credentials")
	}
}
