package reconcile_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/reconcile"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type controlProviderStub struct {
	listReadyCalls    int
	work              workcontrol.WorkItem
	run               workcontrol.Run
	claimCalls        int
	lostClaimResponse bool
	appendFailures    int
	events            map[string]workcontrol.ProjectedEvent
	commands          []workcontrol.Command
	acknowledgements  int
	listReadyStarted  chan struct{}
	listReadyRelease  chan struct{}
	listReadyOnce     sync.Once
}

func (provider *controlProviderStub) Health(context.Context) error { return nil }
func (provider *controlProviderStub) ListReadyWork(context.Context) ([]workcontrol.WorkItem, error) {
	provider.listReadyCalls++
	if provider.listReadyStarted != nil {
		provider.listReadyOnce.Do(func() { close(provider.listReadyStarted) })
		<-provider.listReadyRelease
	}
	if provider.work.Status == workcontrol.WorkReady {
		return []workcontrol.WorkItem{provider.work}, nil
	}
	return []workcontrol.WorkItem{}, nil
}

func TestDisabledIntakeLeavesReadyWorkUnclaimed(t *testing.T) {
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "intake.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	identity, err := journal.EnsureInstallationIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "project-1", Provider: "appdeck",
		Repository: "disabled://m1", Runtime: "disabled:m1", PolicyVersion: "m1", ContextConfig: "disabled:m1",
		ArtifactConfig: "disabled:m1", DeliveryConfig: "disabled:m1", BudgetMinutes: 20, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	provider := &controlProviderStub{work: workcontrol.WorkItem{ID: "task-1", ProjectID: "project-1",
		Title: "Wait for configuration", AcceptanceCriteria: "No claim", Status: workcontrol.WorkReady,
		BudgetMinutes: 10, Version: 1}, events: map[string]workcontrol.ProjectedEvent{}}
	controller, err := reconcile.NewControlController(provider, journal, binding, identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	controller.WithIntakeEnabled(false)
	for range 2 {
		cycle := controller.Cycle(ctx)
		if len(cycle.Errors) != 0 || cycle.Acquisitions != 0 {
			t.Fatalf("disabled cycle = %#v", cycle)
		}
	}
	if provider.listReadyCalls != 0 || provider.claimCalls != 0 {
		t.Fatalf("disabled intake discovered %d times and claimed %d times", provider.listReadyCalls, provider.claimCalls)
	}
	controller.WithIntakeEnabled(true)
	cycle := controller.Cycle(ctx)
	if len(cycle.Errors) != 0 || cycle.Acquisitions != 1 || provider.claimCalls != 1 {
		t.Fatalf("enabled cycle = %#v, claims = %d", cycle, provider.claimCalls)
	}
}
func (provider *controlProviderStub) GetWork(context.Context, string) (workcontrol.WorkDetail, error) {
	runs := []workcontrol.Run{}
	if provider.run.ID != "" {
		runs = append(runs, provider.run)
	}
	return workcontrol.WorkDetail{Work: provider.work, Runs: runs}, nil
}
func (provider *controlProviderStub) LookupClaim(context.Context, string, string) (workcontrol.Run, error) {
	if provider.run.ID == "" {
		return workcontrol.Run{}, errors.New("claim not found")
	}
	return provider.run, nil
}
func (provider *controlProviderStub) ListAssignedRuns(context.Context, string, string, string, int) (workcontrol.ActiveRunPage, error) {
	if provider.run.ID == "" {
		return workcontrol.ActiveRunPage{}, nil
	}
	return workcontrol.ActiveRunPage{Items: []workcontrol.ActiveRun{{Run: provider.run, Work: provider.work}}}, nil
}
func (provider *controlProviderStub) ClaimWork(_ context.Context, workID string, claim workcontrol.Claim) (workcontrol.Run, error) {
	provider.claimCalls++
	if provider.run.ID == "" {
		now := time.Now().UTC()
		lease := now.Add(claim.Lease)
		acceptanceDigest := sha256.Sum256([]byte(provider.work.AcceptanceCriteria))
		provider.run = workcontrol.Run{ID: "run-control-1", WorkItemID: workID, AgentID: claim.AgentID,
			Version: 1, FenceToken: 1, TaskVersion: claim.ExpectedTaskVersion,
			AcceptanceCriteriaDigest: hex.EncodeToString(acceptanceDigest[:]), Status: workcontrol.RunPlanning,
			LeaseExpiresAt: &lease, StartedAt: now, UpdatedAt: now}
		provider.work.Status = workcontrol.WorkStatus(workcontrol.RunPlanning)
	}
	if provider.lostClaimResponse {
		provider.lostClaimResponse = false
		return workcontrol.Run{}, context.DeadlineExceeded
	}
	return provider.run, nil
}
func (provider *controlProviderStub) ListRunCommands(_ context.Context, _ string, after int64, _ int) ([]workcontrol.Command, error) {
	result := make([]workcontrol.Command, 0)
	for _, command := range provider.commands {
		if command.Sequence > after {
			result = append(result, command)
		}
	}
	return result, nil
}
func (provider *controlProviderStub) AcknowledgeRunCommand(_ context.Context, _ string, commandID string, acknowledgement workcontrol.CommandAcknowledgement) error {
	var matched workcontrol.Command
	for _, command := range provider.commands {
		if command.ID == commandID {
			matched = command
			break
		}
	}
	if matched.ID == "" || acknowledgement.ExpectedVersion != provider.run.Version || acknowledgement.FenceToken != provider.run.FenceToken {
		return execution.ErrConflict
	}
	provider.acknowledgements++
	provider.run.Version++
	if matched.Type == "cancel" {
		provider.run.Status = workcontrol.RunCancelled
		provider.run.LeaseExpiresAt = nil
		provider.work.Status = workcontrol.WorkStatus(workcontrol.RunCancelled)
	}
	return nil
}
func (provider *controlProviderStub) AppendRunEvent(_ context.Context, _ string, event workcontrol.ProjectedEvent) error {
	if provider.appendFailures > 0 {
		provider.appendFailures--
		return context.DeadlineExceeded
	}
	if event.ExpectedVersion != provider.run.Version || event.FenceToken != provider.run.FenceToken {
		return execution.ErrConflict
	}
	if existing, ok := provider.events[event.ProducerEventID]; ok {
		if existing.Message != event.Message {
			return execution.ErrConflict
		}
		return nil
	}
	provider.events[event.ProducerEventID] = event
	provider.run.Version++
	return nil
}

func TestControlCycleRecoversLostClaimPublishesOutboxAndConvergesCancellation(t *testing.T) {
	ctx := context.Background()
	journalNow := time.Now().UTC()
	databasePath := filepath.Join(t.TempDir(), "milestone-one.db")
	journal, err := sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return journalNow }))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := journal.EnsureInstallationIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "project-1", Provider: "appdeck",
		Repository: "disabled://m1", Runtime: "disabled:m1", PolicyVersion: "m1", ContextConfig: "disabled:m1",
		ArtifactConfig: "disabled:m1", DeliveryConfig: "disabled:m1", BudgetMinutes: 20, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	sameBinding, err := journal.EnsureProjectBinding(ctx, binding)
	if err != nil || sameBinding.Version != binding.Version {
		t.Fatalf("binding replay = %#v, %v", sameBinding, err)
	}
	provider := &controlProviderStub{work: workcontrol.WorkItem{ID: "task-1", ProjectID: "project-1", Title: "Bounded work",
		AcceptanceCriteria: "one execution", Status: workcontrol.WorkReady, BudgetMinutes: 10, Version: 3,
		CreatedAt: journalNow, UpdatedAt: journalNow}, lostClaimResponse: true, appendFailures: 1, events: map[string]workcontrol.ProjectedEvent{}}
	controller, err := reconcile.NewControlController(provider, journal, binding, identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	first := controller.Cycle(ctx)
	if provider.claimCalls != 1 || len(first.Errors) == 0 {
		t.Fatalf("first cycle = %#v, claims=%d", first, provider.claimCalls)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journalNow = journalNow.Add(2 * time.Second)
	journal, err = sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return journalNow }))
	if err != nil {
		t.Fatal(err)
	}
	controller, err = reconcile.NewControlController(provider, journal, binding, identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	controller.WithIntakeEnabled(false)
	second := controller.Cycle(ctx)
	if provider.claimCalls != 1 || second.Acquisitions != 1 || len(second.Errors) == 0 {
		t.Fatalf("recovery cycle = %#v, claims=%d", second, provider.claimCalls)
	}
	records, err := journal.ListActiveExecutions(ctx, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("active = %#v, %v", records, err)
	}
	record := records[0]
	if record.ExternalRunID != provider.run.ID || record.TaskVersion != provider.work.Version || record.TaskDigest == "" || record.BindingVersion != binding.Version {
		t.Fatalf("bound execution = %#v", record)
	}
	journalNow = journalNow.Add(2 * time.Second)
	third := controller.Cycle(ctx)
	if len(third.Errors) != 0 || third.Published != 1 || len(provider.events) != 1 {
		t.Fatalf("outbox replay = %#v, events=%d", third, len(provider.events))
	}

	provider.run.Version++
	provider.run.FenceToken++
	provider.commands = []workcontrol.Command{{ID: "command-cancel-1", RunID: provider.run.ID, Sequence: 1, Type: "cancel",
		Status: "pending", Reason: "operator stop", RunVersion: provider.run.Version, FenceToken: provider.run.FenceToken}}
	cancelCycle := controller.Cycle(ctx)
	if len(cancelCycle.Errors) != 0 || cancelCycle.Commands != 1 || provider.acknowledgements != 1 {
		t.Fatalf("cancel cycle = %#v, acks=%d", cancelCycle, provider.acknowledgements)
	}
	finished, err := journal.GetExecution(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != execution.StateCancelled || !finished.State.Terminal() {
		t.Fatalf("cancelled execution = %#v", finished)
	}
	replay := controller.Cycle(ctx)
	if provider.acknowledgements != 1 || replay.Commands != 0 {
		t.Fatalf("cancel replay = %#v, acks=%d", replay, provider.acknowledgements)
	}
	provider.commands = append(provider.commands, workcontrol.Command{ID: "command-retry-2", RunID: provider.run.ID,
		Sequence: 2, Type: "retry", Status: "pending", Reason: "operator requested a fresh attempt",
		RunVersion: provider.run.Version, FenceToken: provider.run.FenceToken})
	retryCycle := controller.Cycle(ctx)
	if len(retryCycle.Errors) != 0 || retryCycle.Commands != 1 || provider.acknowledgements != 2 {
		t.Fatalf("terminal retry command = %#v, acks=%d", retryCycle, provider.acknowledgements)
	}
	finished, err = journal.GetExecution(ctx, record.ID)
	if err != nil || finished.CommandCursor != 2 || finished.State != execution.StateCancelled {
		t.Fatalf("terminal command cursor = %#v, %v", finished, err)
	}

	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return journalNow }))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopenedIdentity, err := reopened.EnsureInstallationIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reopenedIdentity.InstallationID != identity.InstallationID || reopenedIdentity.LogicalAgentID != identity.LogicalAgentID {
		t.Fatalf("identity changed across restart")
	}
}

func TestControlCycleAcksCancelAfterHeartbeatAdvancesVersion(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "cancel-after-heartbeat.db"), sqlite.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	identity, err := journal.EnsureInstallationIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "project-cancel-heartbeat", Provider: "appdeck",
		Repository: "disabled://m1", Runtime: "disabled:m1", PolicyVersion: "m1", ContextConfig: "disabled:m1",
		ArtifactConfig: "disabled:m1", DeliveryConfig: "disabled:m1", BudgetMinutes: 20, MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	provider := &controlProviderStub{work: workcontrol.WorkItem{ID: "task-cancel-heartbeat", ProjectID: binding.ProjectID,
		Title: "Cancel after heartbeat", AcceptanceCriteria: "ack converges", Status: workcontrol.WorkReady,
		BudgetMinutes: 10, Version: 2, CreatedAt: now, UpdatedAt: now}, events: map[string]workcontrol.ProjectedEvent{}}
	controller, err := reconcile.NewControlController(provider, journal, binding, identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claimed := controller.Cycle(ctx)
	if len(claimed.Errors) != 0 || claimed.Acquisitions != 1 || claimed.Published != 1 {
		t.Fatalf("claim cycle = %#v", claimed)
	}
	records, err := journal.ListActiveExecutions(ctx, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("active = %#v, %v", records, err)
	}
	commandVersion := provider.run.Version
	commandFence := provider.run.FenceToken
	provider.run.Version++
	if _, err := journal.UpdateProviderAuthority(ctx, records[0].ID, records[0].Version, provider.run.Version,
		provider.run.FenceToken, *provider.run.LeaseExpiresAt, "heartbeat advanced authority before command consume"); err != nil {
		t.Fatal(err)
	}
	provider.commands = []workcontrol.Command{{ID: "command-cancel-stale-epoch", RunID: provider.run.ID, Sequence: 1, Type: "cancel",
		Status: "pending", Reason: "operator stop after heartbeat", RunVersion: commandVersion, FenceToken: commandFence}}
	cancelCycle := controller.Cycle(ctx)
	if len(cancelCycle.Errors) != 0 || cancelCycle.Commands != 1 || provider.acknowledgements != 1 {
		t.Fatalf("cancel after heartbeat = %#v, acks=%d", cancelCycle, provider.acknowledgements)
	}
	finished, err := journal.GetExecution(ctx, records[0].ID)
	if err != nil || finished.State != execution.StateCancelled {
		t.Fatalf("cancelled execution = %#v, %v", finished, err)
	}
}

func TestControlCycleNeverBindsClaimReapedBeforeRecovery(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "reaped-claim.db"), sqlite.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	identity, _ := journal.EnsureInstallationIdentity(ctx)
	binding, _ := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "project-reaped", Provider: "appdeck",
		Repository: "disabled://m1", Runtime: "disabled:m1", PolicyVersion: "m1", ContextConfig: "disabled:m1",
		ArtifactConfig: "disabled:m1", DeliveryConfig: "disabled:m1", BudgetMinutes: 20, MaxConcurrency: 1})
	provider := &controlProviderStub{work: workcontrol.WorkItem{ID: "task-reaped", ProjectID: binding.ProjectID,
		Title: "Do not resurrect", AcceptanceCriteria: "remain fenced", Status: workcontrol.WorkReady,
		BudgetMinutes: 10, Version: 1, CreatedAt: now, UpdatedAt: now}, events: map[string]workcontrol.ProjectedEvent{}}
	intent, _, err := journal.EnsureAcquisitionIntent(ctx, execution.NewAcquisitionIntent{ProjectID: binding.ProjectID,
		WorkID: provider.work.ID, TaskVersion: provider.work.Version, TaskDigest: "digest", AcceptanceCriteriaDigest: "acceptance-digest", ClaimKey: "claim:reaped",
		AgentID: identity.LogicalAgentID, LeaseSeconds: 60, BudgetMinutes: 10})
	if err != nil {
		t.Fatal(err)
	}
	provider.run = workcontrol.Run{ID: "run-reaped", WorkItemID: provider.work.ID, AgentID: identity.LogicalAgentID,
		Version: 2, FenceToken: 2, TaskVersion: provider.work.Version, Status: workcontrol.RunFailed,
		StartedAt: now.Add(-time.Minute), UpdatedAt: now}
	expiredLease := now.Add(-time.Minute)
	if _, err := journal.RecordAcquisitionClaim(ctx, intent.ID, 1, 1, provider.run.ID, expiredLease); err != nil {
		t.Fatal(err)
	}
	controller, err := reconcile.NewControlController(provider, journal, binding, identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cycle := controller.Cycle(ctx)
	if len(cycle.Errors) == 0 || cycle.Acquisitions != 0 || provider.claimCalls != 0 {
		t.Fatalf("reaped recovery = %#v, claims=%d", cycle, provider.claimCalls)
	}
	if _, err := journal.GetExecution(ctx, intent.ID); !errors.Is(err, execution.ErrNotFound) {
		// Acquisition IDs and execution IDs are distinct, so any successful read
		// here would prove an accidental bind under the fenced claim.
		t.Fatalf("reaped claim unexpectedly bound: %v", err)
	}
	if active, err := journal.ListActiveExecutions(ctx, 10); err != nil || len(active) != 0 {
		t.Fatalf("active after reaping = %#v, %v", active, err)
	}
}

func TestControlCycleRecoversDurableIntentBeforeClaimAndClaimedRunBeforeBind(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	for _, crashPoint := range []string{"before_claim", "before_bind"} {
		t.Run(crashPoint, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), crashPoint+".db")
			journal, err := sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return now }))
			if err != nil {
				t.Fatal(err)
			}
			identity, _ := journal.EnsureInstallationIdentity(ctx)
			binding, _ := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "project-" + crashPoint, Provider: "appdeck",
				Repository: "disabled://m1", Runtime: "disabled:m1", PolicyVersion: "m1", ContextConfig: "disabled:m1",
				ArtifactConfig: "disabled:m1", DeliveryConfig: "disabled:m1", BudgetMinutes: 20, MaxConcurrency: 1})
			provider := &controlProviderStub{work: workcontrol.WorkItem{ID: "task-" + crashPoint, ProjectID: binding.ProjectID,
				Title: "Recover crash boundary", AcceptanceCriteria: "exactly one", Status: workcontrol.WorkReady,
				BudgetMinutes: 10, Version: 4, CreatedAt: now, UpdatedAt: now}, events: map[string]workcontrol.ProjectedEvent{}}
			acceptanceDigest := sha256.Sum256([]byte(provider.work.AcceptanceCriteria))
			acceptanceDigestText := hex.EncodeToString(acceptanceDigest[:])
			intent, _, err := journal.EnsureAcquisitionIntent(ctx, execution.NewAcquisitionIntent{ProjectID: binding.ProjectID,
				WorkID: provider.work.ID, TaskVersion: provider.work.Version, TaskDigest: "durable-digest", AcceptanceCriteriaDigest: acceptanceDigestText,
				ClaimKey: "claim:" + crashPoint, AgentID: identity.LogicalAgentID, LeaseSeconds: 60, BudgetMinutes: 10})
			if err != nil {
				t.Fatal(err)
			}
			if crashPoint == "before_bind" {
				lease := now.Add(time.Minute)
				provider.run = workcontrol.Run{ID: "run-before-bind", WorkItemID: provider.work.ID, AgentID: identity.LogicalAgentID,
					Version: 1, FenceToken: 1, TaskVersion: provider.work.Version, AcceptanceCriteriaDigest: acceptanceDigestText, Status: workcontrol.RunPlanning,
					LeaseExpiresAt: &lease, StartedAt: now, UpdatedAt: now}
				provider.work.Status = workcontrol.WorkStatus(workcontrol.RunPlanning)
				if _, err := journal.RecordAcquisitionClaim(ctx, intent.ID, provider.run.Version, provider.run.FenceToken, provider.run.ID, lease); err != nil {
					t.Fatal(err)
				}
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			journal, err = sqlite.Open(ctx, databasePath, sqlite.WithClock(func() time.Time { return now }))
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			controller, err := reconcile.NewControlController(provider, journal, binding, identity, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			cycle := controller.Cycle(ctx)
			if len(cycle.Errors) != 0 || cycle.Acquisitions != 1 {
				t.Fatalf("recovery cycle = %#v", cycle)
			}
			wantClaims := 1
			if crashPoint == "before_bind" {
				wantClaims = 0
			}
			if provider.claimCalls != wantClaims {
				t.Fatalf("claim calls = %d, want %d", provider.claimCalls, wantClaims)
			}
			active, err := journal.ListActiveExecutions(ctx, 10)
			if err != nil || len(active) != 1 || active[0].ExternalRunID != provider.run.ID {
				t.Fatalf("recovered executions = %#v, %v", active, err)
			}
		})
	}
}

func TestControlControllerRejectsOverlappingCycles(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "overlap.db"), sqlite.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	identity, _ := journal.EnsureInstallationIdentity(ctx)
	binding, _ := journal.EnsureProjectBinding(ctx, execution.ProjectBinding{ProjectID: "project-overlap", Provider: "appdeck",
		Repository: "disabled://m1", Runtime: "disabled:m1", PolicyVersion: "m1", ContextConfig: "disabled:m1",
		ArtifactConfig: "disabled:m1", DeliveryConfig: "disabled:m1", BudgetMinutes: 20, MaxConcurrency: 1})
	provider := &controlProviderStub{events: map[string]workcontrol.ProjectedEvent{}, listReadyStarted: make(chan struct{}), listReadyRelease: make(chan struct{})}
	controller, err := reconcile.NewControlController(provider, journal, binding, identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan reconcile.ControlCycle, 1)
	go func() { firstDone <- controller.Cycle(ctx) }()
	<-provider.listReadyStarted
	overlap := controller.Cycle(ctx)
	if len(overlap.Errors) != 1 || !errors.Is(overlap.Errors[0], execution.ErrConflict) {
		t.Fatalf("overlapping cycle = %#v", overlap)
	}
	close(provider.listReadyRelease)
	if first := <-firstDone; len(first.Errors) != 0 {
		t.Fatalf("first cycle = %#v", first)
	}
}
