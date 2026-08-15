package appdeck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/reconcile"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol/conformance"
)

func TestRealAppDeckConformance(t *testing.T) {
	serverURL := os.Getenv("APPDECK_CONFORMANCE_URL")
	fixtureDirectory := os.Getenv("APPDECK_FIXTURES_DIR")
	tokenFile := os.Getenv("APPDECK_CONFORMANCE_TOKEN_FILE")
	if serverURL == "" && fixtureDirectory == "" && tokenFile == "" {
		t.Skip("set APPDECK_CONFORMANCE_URL, APPDECK_FIXTURES_DIR, and APPDECK_CONFORMANCE_TOKEN_FILE to run the real-provider suite")
	}
	if serverURL == "" || fixtureDirectory == "" || tokenFile == "" {
		t.Fatal("APPDECK_CONFORMANCE_URL, APPDECK_FIXTURES_DIR, and APPDECK_CONFORMANCE_TOKEN_FILE must be set together")
	}

	fixtures, err := conformance.LoadFixtures(fixtureDirectory)
	if err != nil {
		t.Fatalf("load AppDeck fixtures: %v", err)
	}
	client, err := New(
		serverURL,
		&http.Client{Timeout: 3 * time.Second},
		WithBearerTokenFile(tokenFile),
	)
	if err != nil {
		t.Fatalf("create AppDeck client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := waitForProvider(ctx, client); err != nil {
		t.Fatalf("wait for real AppDeck process: %v", err)
	}
	unauthenticatedClient, err := New(serverURL, &http.Client{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("create unauthenticated AppDeck client: %v", err)
	}
	_, err = unauthenticatedClient.GetProviderInfo(ctx)
	assertFixtureError(t, err, fixtures.Errors["unauthenticated"])

	seeded, err := seedConformanceTask(ctx, client, fixtures.Task)
	if err != nil {
		t.Fatalf("seed conformance task: %v", err)
	}
	result, err := conformance.Verify(ctx, client, fixtures, seeded.ID)
	if err != nil {
		t.Fatalf("real AppDeck conformance: %v", err)
	}
	if result.Run.WorkItemID != seeded.ID || result.Run.AgentID != fixtures.Agent.ID {
		t.Fatalf("conformance result lost identity: %#v", result)
	}
	verifyMilestoneOneControl(ctx, t, client, fixtures)
	verifyRealMarinaRestart(ctx, t, client, fixtures)
	verifyRealBudgetAndProviderFailure(ctx, t, client, fixtures)
	replayedRun, err := client.ClaimWork(ctx, seeded.ID, workcontrol.Claim{
		IdempotencyKey:      "conformance-claim:" + seeded.ID,
		AgentID:             fixtures.Agent.ID,
		ExpectedTaskVersion: seeded.Version,
		Lease:               2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("replay original claim: %v", err)
	}
	if replayedRun.ID != result.Run.ID || replayedRun.StartedAt != result.Run.StartedAt {
		t.Fatalf("replayed claim = %#v, want original run %#v", replayedRun, result.Run)
	}
	_, err = client.ClaimWork(ctx, seeded.ID, workcontrol.Claim{
		IdempotencyKey:      "conformance-claim:" + seeded.ID,
		AgentID:             fixtures.Agent.ID,
		ExpectedTaskVersion: seeded.Version,
		Lease:               3 * time.Minute,
	})
	assertFixtureError(t, err, fixtures.Errors["conflict"])

	_, err = client.GetWork(ctx, "task_missing_conformance_fixture")
	assertFixtureError(t, err, fixtures.Errors["not_found"])
	_, err = client.ClaimWork(ctx, seeded.ID, workcontrol.Claim{
		IdempotencyKey:      "conformance-second-claim:" + seeded.ID,
		AgentID:             fixtures.Agent.ID,
		ExpectedTaskVersion: seeded.Version,
		Lease:               time.Minute,
	})
	assertFixtureError(t, err, fixtures.Errors["conflict"])
	var ignored []taskDTO
	err = client.do(ctx, http.MethodGet, "/api/v1/tasks?status=not-a-status", nil, &ignored)
	assertFixtureError(t, err, fixtures.Errors["invalid_input"])
	err = client.doIdempotent(
		ctx,
		http.MethodPost,
		"/api/v1/runs/"+result.Run.ID+"/start",
		"conformance-invalid-start:"+seeded.ID,
		runGuardRequest{AgentID: result.Run.AgentID, ExpectedVersion: result.Run.Version, FenceToken: result.Run.FenceToken},
		nil,
	)
	assertFixtureError(t, err, fixtures.Errors["invalid_transition"])
}

func verifyRealMarinaRestart(ctx context.Context, t *testing.T, client *Client, fixtures conformance.Fixtures) {
	t.Helper()
	now := time.Now().UTC()
	databasePath := filepath.Join(t.TempDir(), "marina-restart.db")
	journal, err := sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := journal.EnsureInstallationIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.RegisterAgent(ctx, workcontrol.AgentRegistration{ID: identity.LogicalAgentID,
		InstallationID: identity.InstallationID, Name: "Marina conformance restart", Kind: "controller",
		Capabilities: workcontrol.MilestoneOneCapabilities()}); err != nil {
		t.Fatal(err)
	}
	binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: fixtures.Task.ProjectID,
		Provider: "appdeck", Repository: "disabled://milestone-one", Runtime: "disabled://milestone-one",
		PolicyVersion: "milestone-one", ContextConfig: "disabled://milestone-one",
		ArtifactConfig: "disabled://milestone-one", DeliveryConfig: "disabled://milestone-one",
		BudgetMinutes: 20, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	seeded, err := seedConformanceTask(ctx, client, fixtures.Task)
	if err != nil {
		t.Fatal(err)
	}
	acceptanceDigest := sha256.Sum256([]byte(seeded.AcceptanceCriteria))
	intent, _, err := journal.EnsureAcquisitionIntent(ctx, execution.NewAcquisitionIntent{ProjectID: seeded.ProjectID,
		WorkID: seeded.ID, TaskVersion: seeded.Version, TaskDigest: "task-snapshot:" + seeded.ID,
		AcceptanceCriteriaDigest: hex.EncodeToString(acceptanceDigest[:]), ClaimKey: "restart-claim:" + seeded.ID,
		AgentID: identity.LogicalAgentID, LeaseSeconds: 60, BudgetMinutes: seeded.BudgetMinutes})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	controller, err := reconcile.NewControlController(client, journal, binding, identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cycle := controller.Cycle(ctx)
	if len(cycle.Errors) != 0 || cycle.Acquisitions != 1 || cycle.Published != 1 {
		t.Fatalf("real restart acquisition cycle = %#v", cycle)
	}
	active, err := journal.ListActiveExecutions(ctx, 10)
	if err != nil || len(active) != 1 {
		t.Fatalf("real restart executions = %#v, %v", active, err)
	}
	record := active[0]
	if record.ExternalWorkID != seeded.ID || record.ExternalRunID == "" || record.TaskVersion != seeded.Version ||
		record.FenceToken < 1 || record.ExternalRunVersion < 1 || record.BindingVersion != binding.Version ||
		!record.DeadlineAt.Equal(intent.CreatedAt.Add(time.Duration(intent.BudgetMinutes)*time.Minute)) {
		t.Fatalf("bound real execution = %#v", record)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	persisted, err := journal.GetExecution(ctx, record.ID)
	if err != nil || persisted.ExternalRunID != record.ExternalRunID || persisted.FenceToken != record.FenceToken ||
		persisted.TaskDigest != record.TaskDigest || persisted.DeadlineAt != record.DeadlineAt {
		t.Fatalf("execution after second Marina restart = %#v, %v", persisted, err)
	}
}

func verifyRealBudgetAndProviderFailure(ctx context.Context, t *testing.T, client *Client, fixtures conformance.Fixtures) {
	t.Helper()
	budgetTask, err := seedConformanceTask(ctx, client, fixtures.Task)
	if err != nil {
		t.Fatal(err)
	}
	budgetRun, err := client.ClaimWork(ctx, budgetTask.ID, workcontrol.Claim{IdempotencyKey: "budget-claim:" + budgetTask.ID,
		AgentID: fixtures.Agent.ID, ExpectedTaskVersion: budgetTask.Version, Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	journalNow := time.Now().UTC()
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "budget.db"), sqlite.WithClock(func() time.Time { return journalNow }))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	record, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "appdeck", ProjectID: budgetTask.ProjectID,
		ExternalWorkID: budgetTask.ID, ExternalRunID: budgetRun.ID, AgentID: budgetRun.AgentID,
		ExternalRunVersion: budgetRun.Version, FenceToken: budgetRun.FenceToken, TaskVersion: budgetRun.TaskVersion,
		TaskDigest: "budget-snapshot:" + budgetTask.ID, BindingVersion: 1, LeaseExpiresAt: *budgetRun.LeaseExpiresAt,
		Budget: execution.Budget{DeadlineAt: journalNow.Add(time.Second), MaxAttempts: 1, MaxTokens: 1}})
	if err != nil {
		t.Fatal(err)
	}
	leaseReconciler, err := reconcile.NewLeaseReconciler(client, journal, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	leaseReconciler.WithClock(func() time.Time { return journalNow.Add(2 * time.Second) })
	failed, err := leaseReconciler.StopBudget(ctx, record.ID)
	if err != nil || failed.State != execution.StateFailed || failed.FailureClass != execution.FailureBudget {
		t.Fatalf("real budget stop = %#v, %v", failed, err)
	}
	detail, err := client.GetWork(ctx, budgetTask.ID)
	providerRun, ok := findRunByID(detail.Runs, budgetRun.ID)
	if err != nil || !ok || providerRun.Status != workcontrol.RunBlocked {
		t.Fatalf("real provider budget state = %#v, %v", providerRun, err)
	}

	reapedTask, err := seedConformanceTask(ctx, client, fixtures.Task)
	if err != nil {
		t.Fatal(err)
	}
	reapedRun, err := client.ClaimWork(ctx, reapedTask.ID, workcontrol.Claim{IdempotencyKey: "reaper-claim:" + reapedTask.ID,
		AgentID: fixtures.Agent.ID, ExpectedTaskVersion: reapedTask.Version, Lease: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		detail, err = client.GetWork(ctx, reapedTask.ID)
		providerRun, ok = findRunByID(detail.Runs, reapedRun.ID)
		if err == nil && ok && providerRun.Status == workcontrol.RunFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("provider lease was not reaped: %#v, %v", providerRun, err)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
	if providerRun.Version != reapedRun.Version+1 || providerRun.FenceToken != reapedRun.FenceToken+1 || providerRun.LeaseExpiresAt != nil {
		t.Fatalf("reaped provider authority = %#v", providerRun)
	}
	err = client.RenewRunLease(ctx, reapedRun.ID, workcontrol.LeaseRenewal{IdempotencyKey: "stale-reaper-renew:" + reapedRun.ID,
		AgentID: reapedRun.AgentID, ExpectedVersion: reapedRun.Version, FenceToken: reapedRun.FenceToken, Lease: time.Minute})
	if err == nil {
		t.Fatal("stale lease authority renewed after provider failure")
	}
}

func verifyMilestoneOneControl(ctx context.Context, t *testing.T, client *Client, fixtures conformance.Fixtures) {
	t.Helper()
	seeded, err := seedConformanceTask(ctx, client, fixtures.Task)
	if err != nil {
		t.Fatalf("seed milestone-one task: %v", err)
	}
	claimKey := "milestone-one-claim:" + seeded.ID
	run, err := client.ClaimWork(ctx, seeded.ID, workcontrol.Claim{IdempotencyKey: claimKey,
		AgentID: fixtures.Agent.ID, ExpectedTaskVersion: seeded.Version, Lease: 2 * time.Minute})
	if err != nil {
		t.Fatalf("claim milestone-one task: %v", err)
	}
	recovered, err := client.LookupClaim(ctx, seeded.ProjectID, claimKey)
	if err != nil || recovered.ID != run.ID {
		t.Fatalf("lookup committed claim = %#v, %v", recovered, err)
	}
	assigned, err := client.ListAssignedRuns(ctx, seeded.ProjectID, run.AgentID, "", 10)
	if err != nil || len(assigned.Items) != 1 || assigned.Items[0].Run.ID != run.ID {
		t.Fatalf("assigned runs = %#v, %v", assigned, err)
	}

	projected := workcontrol.ProjectedEvent{IdempotencyKey: "milestone-one-event-1:" + run.ID,
		AgentID: run.AgentID, ExpectedVersion: run.Version, FenceToken: run.FenceToken,
		Producer: "marina", ProducerEventID: "provider-event-1:" + run.ID, ExecutionID: "execution:" + run.ID,
		Sequence: 1, Type: "execution.claimed", Message: "claim durably bound", Payload: map[string]any{"phase": "planning"}, OccurredAt: time.Now().UTC()}
	if err := client.AppendRunEvent(ctx, run.ID, projected); err != nil {
		t.Fatalf("append projected event: %v", err)
	}
	detail, err := client.GetWork(ctx, seeded.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, ok := findRunByID(detail.Runs, run.ID)
	if !ok {
		t.Fatal("projected run disappeared")
	}
	projected.IdempotencyKey = "milestone-one-event-replay:" + run.ID
	projected.ExpectedVersion = current.Version
	if err := client.AppendRunEvent(ctx, run.ID, projected); err != nil {
		t.Fatalf("deduplicate producer event: %v", err)
	}
	afterReplay, _ := client.GetWork(ctx, seeded.ID)
	current, _ = findRunByID(afterReplay.Runs, run.ID)

	var command commandDTO
	if err := client.do(ctx, http.MethodPost, "/api/v1/operator/runs/"+run.ID+"/commands",
		map[string]any{"type": "cancel", "reason": "real-process cancellation proof"}, &command); err != nil {
		t.Fatalf("create cancellation command: %v", err)
	}
	stale := projected
	stale.IdempotencyKey = "milestone-one-stale-event:" + run.ID
	stale.ProducerEventID += ":stale"
	stale.Sequence = 2
	stale.ExpectedVersion = current.Version
	stale.FenceToken = current.FenceToken
	if err := client.AppendRunEvent(ctx, run.ID, stale); err == nil {
		t.Fatal("stale pre-command authority appended an event")
	}
	commands, err := client.ListRunCommands(ctx, run.ID, 0, 10)
	if err != nil || len(commands) != 1 || commands[0].ID != command.ID {
		t.Fatalf("commands = %#v, %v", commands, err)
	}
	ack := workcontrol.CommandAcknowledgement{IdempotencyKey: "milestone-one-cancel-ack:" + run.ID,
		AgentID: run.AgentID, ExpectedVersion: command.RunVersion, FenceToken: command.FenceToken,
		Status: "completed", Result: "authority revoked"}
	if err := client.AcknowledgeRunCommand(ctx, run.ID, command.ID, ack); err != nil {
		t.Fatalf("ack cancellation: %v", err)
	}
	if err := client.AcknowledgeRunCommand(ctx, run.ID, command.ID, ack); err != nil {
		t.Fatalf("replay cancellation acknowledgement: %v", err)
	}
	detail, err = client.GetWork(ctx, seeded.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, ok := findRunByID(detail.Runs, run.ID)
	if !ok || cancelled.Status != workcontrol.RunCancelled {
		t.Fatalf("cancelled run = %#v", cancelled)
	}
	commands, err = client.ListRunCommands(ctx, run.ID, 0, 10)
	if err != nil || commands[0].Status != "completed" {
		t.Fatalf("completed command = %#v, %v", commands, err)
	}
}

func findRunByID(runs []workcontrol.Run, id string) (workcontrol.Run, bool) {
	for _, run := range runs {
		if run.ID == id {
			return run, true
		}
	}
	return workcontrol.Run{}, false
}

func waitForProvider(ctx context.Context, client *Client) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		if _, err := client.GetProviderInfo(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%v: %w", lastErr, ctx.Err())
		case <-ticker.C:
		}
	}
}

func seedConformanceTask(
	ctx context.Context,
	client *Client,
	fixture conformance.TaskFixture,
) (workcontrol.WorkItem, error) {
	request := struct {
		ProjectID          string `json:"project_id"`
		Title              string `json:"title"`
		Description        string `json:"description"`
		AcceptanceCriteria string `json:"acceptance_criteria"`
		Status             string `json:"status"`
		Priority           string `json:"priority"`
		BudgetMinutes      int    `json:"budget_minutes"`
	}{
		ProjectID:          fixture.ProjectID,
		Title:              fixture.Title,
		Description:        fixture.Description,
		AcceptanceCriteria: fixture.AcceptanceCriteria,
		Status:             fixture.Status,
		Priority:           fixture.Priority,
		BudgetMinutes:      fixture.BudgetMinutes,
	}
	var response taskDTO
	if err := client.do(ctx, http.MethodPost, "/api/v1/tasks", request, &response); err != nil {
		return workcontrol.WorkItem{}, err
	}
	return response.toDomain(), nil
}

func assertFixtureError(t *testing.T, err error, fixture conformance.ErrorFixture) {
	t.Helper()
	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("error = %v, want AppDeck APIError", err)
	}
	if apiError.StatusCode != fixture.HTTPStatus ||
		apiError.Code != fixture.Code ||
		apiError.Retryable != fixture.Retryable ||
		apiError.RequestID == "" {
		t.Fatalf("API error = %#v, want fixture semantics %#v", apiError, fixture)
	}
}
