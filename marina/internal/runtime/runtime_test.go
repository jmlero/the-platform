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
