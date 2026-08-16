package verification

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadRepositoryChecksRejectsShellsAndBindsSourceRevision(t *testing.T) {
	repository := t.TempDir()
	manifest := map[string]any{
		"version": "marina-checks/v1",
		"checks": []map[string]any{
			{"id": "unit", "kind": "test", "command": []string{"go", "test", "./..."}},
			{"id": "scan", "kind": "scanner", "command": []string{"git", "diff", "--check"}},
		},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repository, ".marina"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".marina", "checks.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 40)
	checks, err := LoadRepositoryChecks(repository, revision)
	if err != nil {
		t.Fatalf("load checks: %v", err)
	}
	if len(checks) != 2 || checks[0].WorkingDirectory != repository || checks[0].SourceRevision != revision {
		t.Fatalf("checks = %#v", checks)
	}

	manifest["checks"] = []map[string]any{{"id": "unsafe", "kind": "test", "command": []string{"sh", "-c", "go test ./..."}}}
	encoded, _ = json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(repository, ".marina", "checks.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRepositoryChecks(repository, revision); err == nil {
		t.Fatal("shell-based repository check was accepted")
	}
}

func TestRunnerExecutesWithoutShellAgainstExactSourceRevision(t *testing.T) {
	ctx := context.Background()
	repository := filepath.Join(t.TempDir(), "repository")
	runGit(t, ctx, "", "init", repository)
	runGit(t, ctx, repository, "config", "user.email", "marina-test@example.invalid")
	runGit(t, ctx, repository, "config", "user.name", "Marina Test")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("verified\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, ctx, repository, "add", "README.md")
	runGit(t, ctx, repository, "commit", "-m", "fixture")
	revision := strings.TrimSpace(runGit(t, ctx, repository, "rev-parse", "HEAD"))

	runner, err := NewRunner("git", 10*time.Second, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(ctx, Check{
		ID: "git-status", Kind: "scanner", WorkingDirectory: repository,
		SourceRevision: revision, Command: []string{"git", "status", "--porcelain"},
	})
	if err != nil {
		t.Fatalf("run check: %v", err)
	}
	if result.Outcome != OutcomePassed || result.ExitCode != 0 || result.CommandDigest == "" || result.OutputDigest == "" {
		t.Fatalf("check result = %#v", result)
	}
	wrong := strings.Repeat("0", len(revision))
	if _, err := runner.Run(ctx, Check{ID: "wrong", Kind: "test", WorkingDirectory: repository,
		SourceRevision: wrong, Command: []string{"git", "status"}}); err == nil {
		t.Fatal("check ran against the wrong source revision")
	}
}

func TestEvidenceInvalidationAndIndependentReviewAreDeterministic(t *testing.T) {
	now := time.Date(2026, time.August, 16, 13, 0, 0, 0, time.UTC)
	subject := Subject{
		SourceRevision: "abc123", ContextDigest: "sha256:context", PlanRevisionID: "plan-2",
		PolicyVersion: "policy-v3", CommandDigests: map[string]string{"tests": "sha256:command"},
	}
	record := Evidence{
		ID: "evidence-1", Gate: "tests", ProducerID: "runtime:builder", VerifierID: "runtime:reviewer",
		ProducerContextDigest: "sha256:builder-context", VerifierContextDigest: "sha256:reviewer-context",
		SourceRevision: "abc123", ContextDigest: "sha256:context", PlanRevisionID: "plan-2",
		PolicyVersion: "policy-v3", CommandDigest: "sha256:command", Outcome: OutcomePassed,
		ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	current := EvaluateEvidence(now, record, subject, true)
	if current.Status != EvidenceCurrent {
		t.Fatalf("current evidence = %#v", current)
	}

	tests := []struct {
		name   string
		mutate func(*Subject)
		status EvidenceStatus
	}{
		{name: "source", mutate: func(value *Subject) { value.SourceRevision = "def456" }, status: EvidenceSuperseded},
		{name: "context", mutate: func(value *Subject) { value.ContextDigest = "sha256:new-context" }, status: EvidenceSuperseded},
		{name: "plan", mutate: func(value *Subject) { value.PlanRevisionID = "plan-3" }, status: EvidenceSuperseded},
		{name: "policy", mutate: func(value *Subject) { value.PolicyVersion = "policy-v4" }, status: EvidenceSuperseded},
		{name: "command", mutate: func(value *Subject) { value.CommandDigests = map[string]string{"tests": "sha256:new-command"} }, status: EvidenceStale},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := subject
			test.mutate(&changed)
			got := EvaluateEvidence(now, record, changed, true)
			if got.Status != test.status {
				t.Fatalf("evidence status = %q, want %q (%s)", got.Status, test.status, got.Reason)
			}
		})
	}

	selfReviewed := record
	selfReviewed.VerifierID = selfReviewed.ProducerID
	if got := EvaluateEvidence(now, selfReviewed, subject, true); got.Status != EvidenceRejected {
		t.Fatalf("self-reviewed evidence = %#v", got)
	}
	correlatedReview := record
	correlatedReview.VerifierContextDigest = correlatedReview.ProducerContextDigest
	if got := EvaluateEvidence(now, correlatedReview, subject, true); got.Status != EvidenceRejected {
		t.Fatalf("same-context review = %#v", got)
	}
	expired := record
	expired.ExpiresAt = now
	if got := EvaluateEvidence(now, expired, subject, true); got.Status != EvidenceStale {
		t.Fatalf("expired evidence = %#v", got)
	}

	missing := PlanInvalidatedGates(now, nil, subject, true)
	if len(missing) != 1 || missing[0] != "tests" {
		t.Fatalf("missing required gates = %#v", missing)
	}
	reruns := PlanInvalidatedGates(now, []Evidence{record, selfReviewed}, subject, true)
	if len(reruns) != 0 {
		t.Fatalf("a current gate was unnecessarily scheduled: %#v", reruns)
	}
	changed := subject
	changed.SourceRevision = "def456"
	reruns = PlanInvalidatedGates(now, []Evidence{record}, changed, true)
	if len(reruns) != 1 || reruns[0] != "tests" {
		t.Fatalf("invalidated gates = %#v", reruns)
	}
}

func TestIndependentReviewRequirementIsRiskAppropriate(t *testing.T) {
	if RequiresIndependentReview("low") || RequiresIndependentReview("medium") {
		t.Fatal("low and medium risk unexpectedly require an independent runtime")
	}
	if !RequiresIndependentReview("high") || !RequiresIndependentReview("critical") {
		t.Fatal("high and critical risk must require an independent runtime")
	}
}

func runGit(t *testing.T, ctx context.Context, directory string, arguments ...string) string {
	t.Helper()
	command := exec.CommandContext(ctx, "git", arguments...)
	if directory != "" {
		command.Dir = directory
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return string(output)
}
