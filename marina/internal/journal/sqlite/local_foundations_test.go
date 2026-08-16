package sqlite_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/contextpack"
	"github.com/jmlero/the-platform/marina/internal/policy"
)

func TestContextPackagesAndPoliciesAreImmutableAcrossRestart(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "foundations.db")
	store := openStore(t, ctx, path, &now)
	executionRecord, _, err := store.EnsureExecution(ctx, validExecution(now, "run-foundations"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := contextpack.Build(now, contextpack.BuildInput{
		ProjectID: "prj_default", ExecutionID: executionRecord.ID, PlanRevisionID: "plan-1",
		PolicyVersion: "policy-v1", SourceRevision: strings.Repeat("a", 40), HarnessVersion: "harness-v1",
		ToolVersions: map[string]string{"go": "1.26"}, Inputs: foundationContextInputs(now),
	})
	if err != nil {
		t.Fatal(err)
	}
	storedManifest, created, err := store.RecordContextPackage(ctx, manifest)
	if err != nil || !created || storedManifest.Digest != manifest.Digest {
		t.Fatalf("record context = %#v, created %t, err %v", storedManifest, created, err)
	}
	if _, created, err := store.RecordContextPackage(ctx, manifest); err != nil || created {
		t.Fatalf("replay context created=%t err=%v", created, err)
	}

	policySnapshot := policy.Policy{
		ProjectID: "prj_default", Version: "policy-v1", ChangeClass: "code",
		Environment: "development", RiskClass: policy.RiskMedium,
		AllowedActions:   []policy.Action{policy.ActionClaim, policy.ActionRuntimeStart, policy.ActionCompletion},
		AllowedAuthority: []string{"source.read"}, AlwaysHuman: []policy.Action{policy.ActionExpandAuthority},
		Limits: policy.Limits{MaxAttempts: 3, MaxTokens: 100000, MaxCostMicros: 1000000,
			MaxConcurrency: 1, MaxCredentialLifetime: 30 * time.Minute, MaxBlastRadius: policy.BlastRepository},
	}
	storedPolicy, created, err := store.RecordPolicySnapshot(ctx, policySnapshot)
	if err != nil || !created || storedPolicy.Version != policySnapshot.Version {
		t.Fatalf("record policy = %#v, created %t, err %v", storedPolicy, created, err)
	}
	if _, created, err := store.RecordPolicySnapshot(ctx, policySnapshot); err != nil || created {
		t.Fatalf("replay policy created=%t err=%v", created, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, ctx, path, &now)
	t.Cleanup(func() { _ = store.Close() })
	loadedManifest, err := store.GetContextPackage(ctx, manifest.Digest)
	if err != nil || loadedManifest.Digest != manifest.Digest {
		t.Fatalf("loaded context = %#v, err %v", loadedManifest, err)
	}
	loadedPolicy, err := store.GetPolicySnapshot(ctx, "prj_default", "policy-v1", "code", "development", policy.RiskMedium)
	if err != nil || loadedPolicy.Version != policySnapshot.Version {
		t.Fatalf("loaded policy = %#v, err %v", loadedPolicy, err)
	}
}

func foundationContextInputs(now time.Time) []contextpack.Input {
	return []contextpack.Input{
		{Kind: contextpack.KindTaskSnapshot, URI: "artifact://task", Digest: "sha256:task", Trust: contextpack.TrustTrusted, Scope: "task", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
		{Kind: contextpack.KindRepositoryInstructions, URI: "artifact://instructions", Digest: "sha256:instructions", Trust: contextpack.TrustTrusted, Scope: "repository", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
		{Kind: contextpack.KindArchitectureDecision, URI: "artifact://adr", Digest: "sha256:adr", Trust: contextpack.TrustTrusted, Scope: "repository", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
		{Kind: contextpack.KindPolicy, URI: "artifact://policy", Digest: "sha256:policy", Trust: contextpack.TrustTrusted, Scope: "project", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
		{Kind: contextpack.KindSourceRevision, URI: "git://abc123", Digest: "sha256:source", Trust: contextpack.TrustTrusted, Scope: "source", ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)},
		{Kind: contextpack.KindTool, URI: "tool://go/1.26", Digest: "sha256:go", Trust: contextpack.TrustTrusted, Scope: "runtime", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
	}
}
