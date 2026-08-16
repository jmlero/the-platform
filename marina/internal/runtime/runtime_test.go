package runtime

import (
	"testing"
	"time"
)

func TestSpecificationIsBoundedAndContentAddressed(t *testing.T) {
	now := time.Date(2026, time.August, 16, 10, 0, 0, 0, time.UTC)
	specification := Specification{
		ExecutionID: "exec-1", AttemptID: "attempt-1", IdempotencyKey: "runtime-start-1",
		RepositoryURI: "https://example.invalid/repository.git", SourceRevision: "abc123",
		ContextURI: "artifact://contexts/context-1.json", ContextDigest: "sha256:context",
		HarnessVersion: "harness-v1", PolicyVersion: "policy-v3", AuthorityFence: 7,
		DeadlineAt: now.Add(time.Hour), NetworkPolicy: "deny-by-default", FilesystemPolicy: "workspace-only",
		Limits: Limits{CPUMillis: 1000, MemoryBytes: 512 << 20, EphemeralStorageBytes: 2 << 30,
			WallTimeSeconds: 1800, MaxProcesses: 64},
		CredentialReferences: []CredentialReference{
			{Kind: "source", Reference: "secret://source/task-1", ExpiresAt: now.Add(30 * time.Minute)},
			{Kind: "artifact", Reference: "secret://artifact/task-1", ExpiresAt: now.Add(45 * time.Minute)},
		},
	}
	if err := specification.Validate(now); err != nil {
		t.Fatalf("validate specification: %v", err)
	}
	first, err := specification.Digest()
	if err != nil {
		t.Fatalf("first digest: %v", err)
	}
	second, err := specification.Digest()
	if err != nil || first != second || first == "" {
		t.Fatalf("deterministic digest = %q, %q, %v", first, second, err)
	}

	changed := specification
	changed.AuthorityFence++
	changedDigest, err := changed.Digest()
	if err != nil || changedDigest == first {
		t.Fatalf("changed authority digest = %q, original %q, err %v", changedDigest, first, err)
	}

	unbounded := specification
	unbounded.Limits.WallTimeSeconds = 0
	if err := unbounded.Validate(now); err == nil {
		t.Fatal("unbounded specification was accepted")
	}
	overlongCredential := specification
	overlongCredential.CredentialReferences[0].ExpiresAt = specification.DeadlineAt.Add(time.Second)
	if err := overlongCredential.Validate(now); err == nil {
		t.Fatal("credential outliving the runtime was accepted")
	}
}

func TestPlanRecoveryMakesAmbiguousRuntimeBehaviorExplicit(t *testing.T) {
	now := time.Date(2026, time.August, 16, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		attempt      Attempt
		observation  Observation
		request      RecoveryRequest
		capabilities Capabilities
		want         RecoveryAction
	}{
		{name: "start pending attempt", attempt: Attempt{State: StatePending}, want: RecoveryStart},
		{name: "inspect lost start response", attempt: Attempt{State: StateStarting}, observation: Observation{Found: false}, want: RecoveryInspectByKey},
		{name: "bind discovered orphan", attempt: Attempt{State: StateStarting}, observation: Observation{Found: true, ProviderHandle: "provider/job-1", State: StateRunning}, want: RecoveryBindDiscovered},
		{name: "provider disappeared", attempt: Attempt{State: StateRunning, ProviderHandle: "provider/job-1"}, observation: Observation{Found: false}, want: RecoveryFailDisappeared},
		{name: "unsupported resume", attempt: Attempt{State: StatePaused}, observation: Observation{Found: true, State: StatePaused}, request: RecoveryRequest{Resume: true}, want: RecoveryFailUnsupportedResume},
		{name: "supported resume", attempt: Attempt{State: StatePaused}, observation: Observation{Found: true, State: StatePaused}, request: RecoveryRequest{Resume: true}, capabilities: Capabilities{Resume: true}, want: RecoveryResume},
		{name: "graceful cancel", attempt: Attempt{State: StateRunning}, observation: Observation{Found: true, State: StateRunning}, request: RecoveryRequest{Cancel: true, CancelDeadline: now.Add(time.Minute)}, want: RecoveryCancel},
		{name: "forced cancel", attempt: Attempt{State: StateRunning}, observation: Observation{Found: true, State: StateRunning}, request: RecoveryRequest{Cancel: true, CancelDeadline: now.Add(-time.Second)}, want: RecoveryForceCancel},
		{name: "collect outputs", attempt: Attempt{State: StateSucceeded, CleanupState: CleanupPending}, want: RecoveryCollectArtifacts},
		{name: "cleanup terminal attempt", attempt: Attempt{State: StateSucceeded, CleanupState: CleanupPending, OutputsCollected: true}, want: RecoveryCleanup},
		{name: "no work after cleanup", attempt: Attempt{State: StateSucceeded, CleanupState: CleanupCompleted, OutputsCollected: true}, want: RecoveryNone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanRecovery(test.attempt, test.observation, test.request, test.capabilities, now)
			if plan.Action != test.want {
				t.Fatalf("recovery action = %q, want %q (%s)", plan.Action, test.want, plan.Reason)
			}
		})
	}
}
