package reconcile

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type leaseProviderStub struct {
	healthErr error
	details   []workcontrol.WorkDetail
	getCalls  int
	renewals  []workcontrol.LeaseRenewal
	renewErr  error
	blocks    []string
	blockErr  error
}

func (provider *leaseProviderStub) Health(context.Context) error {
	return provider.healthErr
}

func (provider *leaseProviderStub) GetWork(context.Context, string) (workcontrol.WorkDetail, error) {
	index := provider.getCalls
	provider.getCalls++
	if len(provider.details) == 0 {
		return workcontrol.WorkDetail{}, errors.New("no work detail configured")
	}
	if index >= len(provider.details) {
		index = len(provider.details) - 1
	}
	return provider.details[index], nil
}

func (provider *leaseProviderStub) RenewRunLease(_ context.Context, _ string, renewal workcontrol.LeaseRenewal) error {
	provider.renewals = append(provider.renewals, renewal)
	return provider.renewErr
}

func (provider *leaseProviderStub) BlockRun(_ context.Context, _ string, reason string) error {
	provider.blocks = append(provider.blocks, reason)
	return provider.blockErr
}

type leaseJournalStub struct {
	record          execution.Execution
	reconcileCalls  int
	conflictOnce    bool
	transitionCalls int
}

func (journal *leaseJournalStub) Transition(
	_ context.Context,
	_ string,
	input execution.Transition,
) (execution.Execution, error) {
	journal.transitionCalls++
	if input.ExpectedVersion != journal.record.Version {
		return execution.Execution{}, execution.ErrConflict
	}
	if !journal.record.State.CanTransition(input.Target) {
		return execution.Execution{}, execution.ErrInvalidTransition
	}
	journal.record.State = input.Target
	journal.record.FailureClass = input.FailureClass
	journal.record.FailureMessage = input.FailureMessage
	journal.record.Version++
	return journal.record, nil
}

func (journal *leaseJournalStub) GetExecution(context.Context, string) (execution.Execution, error) {
	return journal.record, nil
}

func (journal *leaseJournalStub) ReconcileLease(
	_ context.Context,
	_ string,
	expectedVersion int64,
	expiresAt time.Time,
	_ string,
) (execution.Execution, error) {
	journal.reconcileCalls++
	if journal.conflictOnce {
		journal.conflictOnce = false
		journal.record.Version++
		return execution.Execution{}, execution.ErrConflict
	}
	if expectedVersion != journal.record.Version {
		return execution.Execution{}, execution.ErrConflict
	}
	journal.record.LeaseExpiresAt = expiresAt
	journal.record.Version++
	return journal.record, nil
}

func TestLeaseReconcilerConfirmsOwnershipAndPersistsProviderTruth(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	record := activeExecution(now)
	providerExpiry := now.Add(20 * time.Minute)
	provider := &leaseProviderStub{details: []workcontrol.WorkDetail{
		ownedWork(record, workcontrol.RunRunning, &providerExpiry),
	}}
	journal := &leaseJournalStub{record: record}
	reconciler, err := NewLeaseReconciler(provider, journal, 30*time.Second)
	if err != nil {
		t.Fatalf("new reconciler: %v", err)
	}
	reconciler.WithClock(func() time.Time { return now })

	result, err := reconciler.Reconcile(context.Background(), record.ID, 2*time.Minute)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Status != LeaseConfirmed || result.LeaseExpiresAt == nil || !result.LeaseExpiresAt.Equal(providerExpiry) {
		t.Fatalf("result = %#v", result)
	}
	if len(provider.renewals) != 0 {
		t.Fatalf("unexpected heartbeats = %#v", provider.renewals)
	}
	if journal.reconcileCalls != 1 || !journal.record.LeaseExpiresAt.Equal(providerExpiry) {
		t.Fatalf("journal did not persist provider lease: %#v", journal)
	}
}

func TestLeaseReconcilerDoesNotTouchWorkWhenProviderIsIncompatible(t *testing.T) {
	now := time.Date(2026, time.July, 18, 10, 0, 0, 0, time.UTC)
	record := activeExecution(now)
	provider := &leaseProviderStub{healthErr: errors.New("missing run.lease.renew capability")}
	journal := &leaseJournalStub{record: record}
	reconciler, err := NewLeaseReconciler(provider, journal, 30*time.Second)
	if err != nil {
		t.Fatalf("new reconciler: %v", err)
	}

	_, err = reconciler.Reconcile(context.Background(), record.ID, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "verify provider compatibility") {
		t.Fatalf("reconcile error = %v", err)
	}
	if provider.getCalls != 0 || len(provider.renewals) != 0 || len(provider.blocks) != 0 {
		t.Fatalf("incompatible provider received work mutations: %#v", provider)
	}
}

func TestLeaseReconcilerRenewsExpiredLeaseAndConfirmsResponse(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	record := activeExecution(now)
	expired := now.Add(-time.Minute)
	renewed := now.Add(10 * time.Minute)
	provider := &leaseProviderStub{details: []workcontrol.WorkDetail{
		ownedWork(record, workcontrol.RunRunning, &expired),
		ownedWork(record, workcontrol.RunRunning, &renewed),
	}}
	journal := &leaseJournalStub{record: record, conflictOnce: true}
	reconciler, err := NewLeaseReconciler(provider, journal, 45*time.Second)
	if err != nil {
		t.Fatalf("new reconciler: %v", err)
	}
	reconciler.WithClock(func() time.Time { return now })

	result, err := reconciler.Reconcile(context.Background(), record.ID, 2*time.Minute)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Status != LeaseRenewed || result.LeaseExpiresAt == nil || !result.LeaseExpiresAt.Equal(renewed) {
		t.Fatalf("result = %#v", result)
	}
	if len(provider.renewals) != 1 || provider.renewals[0].AgentID != record.AgentID || provider.renewals[0].Lease != 45*time.Second {
		t.Fatalf("renewals = %#v", provider.renewals)
	}
	if provider.getCalls != 2 || journal.reconcileCalls != 2 {
		t.Fatalf("get calls = %d, journal calls = %d", provider.getCalls, journal.reconcileCalls)
	}
}

func TestLeaseReconcilerConvergesProviderTerminalState(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	record := activeExecution(now)
	provider := &leaseProviderStub{details: []workcontrol.WorkDetail{
		ownedWork(record, workcontrol.RunFailed, nil),
	}}
	journal := &leaseJournalStub{record: record}
	reconciler, err := NewLeaseReconciler(provider, journal, 30*time.Second)
	if err != nil {
		t.Fatalf("new reconciler: %v", err)
	}

	result, err := reconciler.Reconcile(context.Background(), record.ID, time.Minute)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Status != LeaseProviderTerminal || result.ProviderRunStatus != workcontrol.RunFailed {
		t.Fatalf("result = %#v", result)
	}
	if len(provider.renewals) != 0 || journal.reconcileCalls != 0 || journal.transitionCalls != 1 {
		t.Fatalf("terminal run caused mutations: provider=%#v journal=%#v", provider, journal)
	}
	if journal.record.State != execution.StateFailed || journal.record.FailureClass != execution.FailureExternal {
		t.Fatalf("terminal journal state = %#v", journal.record)
	}
}

func TestLeaseReconcilerDoesNotBypassCompletionLifecycle(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	record := activeExecution(now)
	provider := &leaseProviderStub{details: []workcontrol.WorkDetail{
		ownedWork(record, workcontrol.RunCompleted, nil),
	}}
	journal := &leaseJournalStub{record: record}
	reconciler, err := NewLeaseReconciler(provider, journal, 30*time.Second)
	if err != nil {
		t.Fatalf("new reconciler: %v", err)
	}
	result, err := reconciler.Reconcile(context.Background(), record.ID, time.Minute)
	if !errors.Is(err, execution.ErrInvalidTransition) {
		t.Fatalf("completed provider reconciliation error = %v, want invalid transition", err)
	}
	if result.Status != LeaseProviderTerminal || journal.record.State != execution.StateRunning {
		t.Fatalf("result = %#v, record = %#v", result, journal.record)
	}
}

func TestLeaseReconcilerStopsExhaustedBudgetOnProviderAndJournal(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	record := activeExecution(now)
	record.DeadlineAt = now
	activeExpiry := now.Add(time.Minute)
	blocked := ownedWork(record, workcontrol.RunBlocked, nil)
	blocked.Runs[0].ResultSummary = budgetBlockReason
	provider := &leaseProviderStub{details: []workcontrol.WorkDetail{
		ownedWork(record, workcontrol.RunRunning, &activeExpiry),
		blocked,
	}}
	journal := &leaseJournalStub{record: record}
	reconciler, err := NewLeaseReconciler(provider, journal, 30*time.Second)
	if err != nil {
		t.Fatalf("new reconciler: %v", err)
	}
	reconciler.WithClock(func() time.Time { return now })

	updated, err := reconciler.StopBudget(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("stop budget: %v", err)
	}
	if len(provider.blocks) != 1 || provider.blocks[0] != budgetBlockReason {
		t.Fatalf("provider blocks = %#v", provider.blocks)
	}
	if updated.State != execution.StateFailed || updated.FailureClass != execution.FailureBudget {
		t.Fatalf("updated execution = %#v", updated)
	}
}

func TestLeaseReconcilerRejectsOwnershipMismatchWithoutHeartbeat(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	record := activeExecution(now)
	expiry := now.Add(time.Minute)
	detail := ownedWork(record, workcontrol.RunRunning, &expiry)
	detail.Runs[0].AgentID = "another-agent"
	provider := &leaseProviderStub{details: []workcontrol.WorkDetail{detail}}
	journal := &leaseJournalStub{record: record}
	reconciler, err := NewLeaseReconciler(provider, journal, 30*time.Second)
	if err != nil {
		t.Fatalf("new reconciler: %v", err)
	}

	if _, err := reconciler.Reconcile(context.Background(), record.ID, 2*time.Minute); !errors.Is(err, ErrExternalOwnershipMismatch) {
		t.Fatalf("reconcile error = %v, want ownership mismatch", err)
	}
	if len(provider.renewals) != 0 || journal.reconcileCalls != 0 {
		t.Fatalf("ownership mismatch caused mutations: provider=%#v journal=%#v", provider, journal)
	}
}

func TestLeaseReconcilerRejectsMissingRunAndInvalidConfiguration(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	record := activeExecution(now)
	provider := &leaseProviderStub{details: []workcontrol.WorkDetail{{
		Work: workcontrol.WorkItem{ID: record.ExternalWorkID},
	}}}
	journal := &leaseJournalStub{record: record}
	reconciler, err := NewLeaseReconciler(provider, journal, time.Second)
	if err != nil {
		t.Fatalf("new reconciler: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), record.ID, 0); !errors.Is(err, ErrExternalRunMissing) {
		t.Fatalf("missing run error = %v", err)
	}
	if _, err := NewLeaseReconciler(nil, journal, time.Second); err == nil {
		t.Fatal("nil provider was accepted")
	}
	if _, err := NewLeaseReconciler(provider, nil, time.Second); err == nil {
		t.Fatal("nil journal was accepted")
	}
	if _, err := NewLeaseReconciler(provider, journal, time.Millisecond); err == nil {
		t.Fatal("short lease was accepted")
	}
}

func activeExecution(now time.Time) execution.Execution {
	return execution.Execution{
		ID:             "exec-1",
		ExternalWorkID: "task-1",
		ExternalRunID:  "run-1",
		AgentID:        "marina-1",
		State:          execution.StateRunning,
		Attempt:        1,
		LeaseExpiresAt: now.Add(5 * time.Minute),
		DeadlineAt:     now.Add(time.Hour),
		MaxAttempts:    3,
		MaxTokens:      1000,
		Version:        4,
	}
}

func ownedWork(record execution.Execution, status workcontrol.RunStatus, expiresAt *time.Time) workcontrol.WorkDetail {
	return workcontrol.WorkDetail{
		Work: workcontrol.WorkItem{ID: record.ExternalWorkID},
		Runs: []workcontrol.Run{{
			ID:             record.ExternalRunID,
			WorkItemID:     record.ExternalWorkID,
			AgentID:        record.AgentID,
			Status:         status,
			LeaseExpiresAt: expiresAt,
		}},
	}
}
