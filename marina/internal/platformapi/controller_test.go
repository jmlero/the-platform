package platformapi

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubeapi"
	"github.com/jmlero/the-platform/marina/internal/platformapi/kubefake"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

func controllerFixture(t *testing.T) (*Controller, *kubefake.Server) {
	t.Helper()
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "marina.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { journal.Close() })
	binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "factory.acceptance", Provider: "marina", Repository: "https://github.com/owner/repo.git",
		Runtime: "test", PolicyVersion: "v1", ContextConfig: "cfg", ArtifactConfig: "a", DeliveryConfig: "github", BudgetMinutes: 120, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.RecordRepositorySnapshot(ctx, "factory.acceptance", "digest", workcontrol.RepositoryBinding{Version: binding.Version, Repository: "owner/repo",
		CloneURL: "https://github.com/owner/repo.git", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	fake := kubefake.New()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	kube, err := kubeapi.New(kubeapi.Config{ServerURL: server.URL}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	config := flow.Config{ProjectID: "factory.acceptance", MaxTokens: 400000, BudgetMinutes: 120, Delivery: "fast_forward", RequiredCI: []string{"test"},
		AllowedPaths: []string{"src"}, Checks: []workresult.Check{{ID: "unit", Directory: ".", Command: []string{"node", "--test"}}},
		Model: workerapi.Model{Name: "implementer"}, ReviewerModel: &workerapi.Model{Name: "reviewer"}}
	c := &Controller{Kube: kube, Journal: journal, Platform: journal.Platform(), Projects: map[string]flow.Config{"factory.acceptance": config}, AgentID: "marina"}
	fake.Put("projects", map[string]any{"apiVersion": apiVersion, "kind": "Project", "metadata": map[string]any{"name": "acceptance", "namespace": "factory"},
		"spec": map[string]any{"repository": map[string]any{"provider": "github", "url": "https://github.com/owner/repo.git", "baseBranch": "main",
			"credentialSecret": map[string]any{"name": "git", "key": "token"}}, "defaultWorkload": "land"}})
	fake.Put("workloads", map[string]any{"apiVersion": apiVersion, "kind": "Workload", "metadata": map[string]any{"name": "land", "namespace": "factory"},
		"spec": map[string]any{"type": "repository-change", "repositoryChange": map[string]any{"model": "implementer", "allowedPaths": []any{"src"},
			"checks": []any{map[string]any{"id": "unit", "command": []any{"node", "--test"}}}, "planApproval": "required",
			"review": map[string]any{"enabled": false}, "delivery": map[string]any{"mode": "land-on-main", "requiredChecks": []any{"test"}}, "repair": map[string]any{"maxFailures": 1}}}})
	return c, fake
}

func runObject(name, workload string) map[string]any {
	spec := map[string]any{"project": "acceptance", "title": "Reject an empty range", "acceptanceCriteria": "A test passes",
		"budget": map[string]any{"timeMinutes": 30, "maxTokens": 1000.0, "maxAttempts": 7}}
	if workload != "" {
		spec["workload"] = workload
	}
	return map[string]any{"apiVersion": apiVersion, "kind": "Run", "metadata": map[string]any{"name": name, "namespace": "factory"}, "spec": spec}
}

func cycle(t *testing.T, c *Controller) {
	t.Helper()
	if problems := c.Cycle(context.Background()); len(problems) != 0 {
		t.Fatalf("cycle problems: %v", problems)
	}
}

func phase(object map[string]any) string {
	status, _ := object["status"].(map[string]any)
	value, _ := status["phase"].(string)
	return value
}

func TestControllerAcceptsRunsWithTheirWorkloadChoicesAndExplainsWaits(t *testing.T) {
	c, fake := controllerFixture(t)
	ctx := context.Background()
	fake.Put("runs", runObject("task-1", ""))
	fake.Put("runs", runObject("task-2", "missing"))
	cycle(t, c)
	work, err := c.Platform.Work(ctx, "factory.task-1")
	if err != nil {
		t.Fatal(err)
	}
	choices := work.Item.Choices
	if choices == nil || choices.Review || !choices.RequireApproval || choices.Delivery != "fast_forward" || choices.RepairFailures != 1 || work.Item.Context.Repository.Repository != "owner/repo" {
		t.Fatalf("frozen work = %+v choices=%+v", work.Item, choices)
	}
	run := fake.Get("runs", "factory/task-1")
	if phase(run) != "Pending" || !strings.Contains(strings.Join(anyStrings(run["metadata"].(map[string]any)["finalizers"]), ","), finalizer) {
		t.Fatalf("accepted run = %+v", run)
	}
	waiting := fake.Get("runs", "factory/task-2")
	if _, err := c.Platform.Work(ctx, "factory.task-2"); err == nil || !strings.Contains(waiting["status"].(map[string]any)["reason"].(string), "missing") {
		t.Fatalf("unknown workload accepted: %+v", waiting["status"])
	}
	if accepted := fake.Get("workloads", "factory/land")["status"].(map[string]any)["conditions"].([]any)[0].(map[string]any); accepted["status"] != "True" {
		t.Fatalf("workload = %+v", accepted)
	}
	if ready := fake.Get("projects", "factory/acceptance")["status"].(map[string]any)["conditions"].([]any)[0].(map[string]any); ready["reason"] != "Binding" {
		t.Fatalf("project = %+v", ready)
	}
}

func TestControllerPublishesDecisionsAppliesAnswersAndCancels(t *testing.T) {
	c, fake := controllerFixture(t)
	ctx := context.Background()
	fake.Put("runs", runObject("task-1", ""))
	cycle(t, c)
	claimed, err := c.Platform.ClaimWork(ctx, "factory.task-1", workcontrol.Claim{IdempotencyKey: "claim", AgentID: "marina", ExpectedTaskVersion: 1, Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Platform.SubmitPlanRevision(ctx, claimed.ID, workcontrol.PlanRevisionSubmission{IdempotencyKey: "plan", AgentID: "marina", ExpectedVersion: claimed.Version,
		FenceToken: claimed.FenceToken, Plan: "Add a guard", ContextDigest: "sha256:c", RequireApproval: true}); err != nil {
		t.Fatal(err)
	}
	cycle(t, c)
	decision := fake.Get("decisions", "factory/task-1-plan-1")
	if decision == nil || decision["spec"].(map[string]any)["kind"] != "plan-approval" || decision["spec"].(map[string]any)["owner"].(map[string]any)["name"] != "task-1" {
		t.Fatalf("decision resource = %+v", decision)
	}
	if phase(fake.Get("runs", "factory/task-1")) != "WaitingDecision" {
		t.Fatalf("run phase = %s", phase(fake.Get("runs", "factory/task-1")))
	}
	decision["spec"].(map[string]any)["response"] = map[string]any{"answer": "Approve plan", "answeredBy": "system:serviceaccount:marina-system:deck"}
	cycle(t, c)
	if phase(fake.Get("decisions", "factory/task-1-plan-1")) != "Answered" {
		t.Fatalf("decision = %+v", fake.Get("decisions", "factory/task-1-plan-1")["status"])
	}
	if answered, _ := c.Platform.FindDecision(ctx, "factory", "task-1-plan-1"); answered.Decision.AnsweredBy != "system:serviceaccount:marina-system:deck" {
		t.Fatalf("ledger answer = %+v", answered.Decision)
	}
	fake.Put("runs", runObject("task-2", ""))
	cycle(t, c)
	fake.Get("runs", "factory/task-2")["spec"].(map[string]any)["cancel"] = true
	cycle(t, c)
	if phase(fake.Get("runs", "factory/task-2")) != "Cancelled" {
		t.Fatalf("cancelled run = %+v", fake.Get("runs", "factory/task-2")["status"])
	}
	fake.Get("runs", "factory/task-2")["metadata"].(map[string]any)["deletionTimestamp"] = "2026-10-17T19:00:00Z"
	cycle(t, c)
	if gone := fake.Get("runs", "factory/task-2"); gone != nil {
		t.Fatalf("finalizer kept after a terminal delete: %v", gone["metadata"])
	}
}

func anyStrings(value any) []string {
	var out []string
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			out = append(out, item.(string))
		}
	case []string:
		out = v
	}
	return out
}
