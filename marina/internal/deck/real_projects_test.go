package deck

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/reconcile"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

func TestRealProjectProfiles(t *testing.T) {
	url, token, policy := os.Getenv("DECK_CONFORMANCE_URL"), os.Getenv("DECK_CONFORMANCE_TOKEN_FILE"), os.Getenv("DECK_CONFORMANCE_AUTH_FILE")
	if url == "" || token == "" || policy == "" {
		t.Skip("isolated real Deck conformance policy required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := New(url, &http.Client{Timeout: 3 * time.Second}, WithBearerTokenFile(token))
	if err != nil {
		t.Fatal(err)
	}
	operator, err := New(url, &http.Client{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err = waitForProvider(ctx, client); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.WriteFile(policy, original, 0600); err != nil {
			t.Error(err)
		}
	}()
	var auth struct {
		Version    int `json:"version"`
		Identities []struct {
			ID           string   `json:"id"`
			Hashes       []string `json:"token_sha256"`
			Projects     []string `json:"projects"`
			Capabilities []string `json:"capabilities"`
		} `json:"identities"`
	}
	if err = json.Unmarshal(original, &auth); err != nil {
		t.Fatal(err)
	}
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "projects.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	identity, err := journal.EnsureInstallationIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.RegisterAgent(ctx, workcontrol.AgentRegistration{ID: identity.LogicalAgentID, InstallationID: identity.InstallationID, Name: "Two-project fixture"}); err != nil {
		t.Fatal(err)
	}
	bindings := []execution.ProjectBinding{}
	configs := []flow.Config{}
	tasks := []taskDTO{}
	for i, repo := range []string{"example/profile-first", "example/profile-second"} {
		var project struct {
			Project struct {
				ID string `json:"id"`
			} `json:"project"`
			Repository workcontrol.RepositoryRequest `json:"repository"`
		}
		if err = operator.do(ctx, "POST", "/api/v1/operator/projects", map[string]string{"repository": repo, "base_branch": "main"}, &project); err != nil {
			t.Fatal(err)
		}
		auth.Identities[0].Projects = append(auth.Identities[0].Projects, project.Project.ID)
		raw, _ := json.Marshal(auth)
		if err = os.WriteFile(policy, raw, 0600); err != nil {
			t.Fatal(err)
		}
		config := flow.Config{ProjectID: project.Project.ID, PolicyVersion: "project-fixture", BudgetMinutes: 20, MaxTokens: int64(20000 + i*10000), Delivery: "fast_forward", AllowedPaths: []string{"src"}, RequiredCI: []string{"fixture-check"}, Checks: []workresult.Check{{ID: repo, Directory: ".", Command: []string{"node", "--test"}}}, Git: github.Config{Repository: repo, CloneURL: "https://github.com/" + repo + ".git", BaseBranch: "main", TokenFile: "/synthetic/git-" + string(rune('a'+i))}}
		binding, err := flow.InitializeRepository(ctx, journal, config, execution.ProjectBinding{ProjectID: config.ProjectID, Provider: "deck", Runtime: "test", PolicyVersion: config.PolicyVersion, ContextConfig: config.Digest(), ArtifactConfig: "test", DeliveryConfig: "test", BudgetMinutes: 20, MaxConcurrency: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err = client.RecordRepositoryResult(ctx, config.ProjectID, workcontrol.RepositoryResult{RequestRevision: project.Repository.Revision, Status: "connected", Binding: &workcontrol.RepositoryBinding{Version: binding.Version, Repository: repo, CloneURL: config.Git.CloneURL, BaseBranch: "main"}}); err != nil {
			t.Fatal(err)
		}
		if err = client.PublishWorkloads(ctx, config.ProjectID, workcontrol.WorkloadCatalog{Profiles: config.WorkloadProfiles(), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
			t.Fatal(err)
		}
		priority := "low"
		if i == 1 {
			priority = "high"
		}
		var task taskDTO
		if err = operator.doIdempotent(ctx, "POST", "/api/v1/operator/projects/"+config.ProjectID+"/workloads/invoke", "project-profile-"+config.ProjectID, map[string]any{"profile_id": "repository/v2", "profile_digest": config.Digest(), "task": map[string]any{"title": "Configured project task", "description": "Verify the selected project", "acceptance_criteria": "Per-project checks and credentials are retained", "status": "ready", "priority": priority, "budget": map[string]any{"time_minutes": 10, "max_tokens": config.MaxTokens, "max_attempts": 3, "currency": "USD", "max_concurrency": 1, "authority": config.RepositoryAuthority()}}}, &task); err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
		configs = append(configs, config)
		tasks = append(tasks, task)
	}
	control, err := reconcile.NewControlController(client, journal, bindings[0], identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	control.WithProjectBinding(bindings[1])
	for _, config := range configs {
		control.WithProjectExecutionLimits(config.ProjectID, flow.RepositoryRuntimeLimit, config.MaxTokens)
	}
	cycle := control.Cycle(ctx)
	if len(cycle.Errors) > 0 || cycle.Acquisitions != 1 {
		t.Fatalf("project admission: %+v", cycle)
	}
	records, err := journal.ListActiveExecutions(ctx, 10)
	if err != nil || len(records) != 1 || records[0].ProjectID != configs[1].ProjectID || records[0].MaxTokens != configs[1].MaxTokens {
		t.Fatalf("wrong project/budget: %+v %v", records, err)
	}
	snapshot, err := journal.AcquiredTaskSnapshot(ctx, records[0].ProjectID, records[0].ExternalWorkID, records[0].TaskVersion)
	if err != nil {
		t.Fatal(err)
	}
	var frozen workcontrol.WorkItem
	if err = json.Unmarshal([]byte(snapshot), &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.Workload == nil || frozen.Workload.Digest != configs[1].Digest() || frozen.Context.Repository.Repository != configs[1].Git.Repository {
		t.Fatal("cross-project frozen profile")
	}
	effective, err := (&flow.Controller{Config: configs[1], Journal: journal}).ConfigurationForExecution(ctx, records[0])
	if err != nil || effective.Git.TokenFile != configs[1].Git.TokenFile || !strings.Contains(effective.Checks[0].ID, "second") {
		t.Fatalf("selected wrong credentials/checks: %v", err)
	}
	cycle = control.Cycle(ctx)
	if cycle.Acquisitions != 0 {
		t.Fatalf("second active task: %+v", cycle)
	}
	detail, err := client.GetWork(ctx, tasks[0].ID)
	if err != nil || len(detail.Runs) != 0 {
		t.Fatalf("other project claimed concurrently: %+v %v", detail, err)
	}
}
