package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerificationChecksThePinnedRevisionInAFreshWorkspace(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet")
	if err := os.WriteFile(filepath.Join(repo, "answer.txt"), []byte("expected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "answer.txt")
	git("commit", "--quiet", "-m", "fixture")
	head := git("rev-parse", "HEAD")
	input := Input{Stage: "verify", SourceURL: repo, SourceRevision: head, Checks: []Check{{ID: "regression", Directory: ".", Command: []string{"git", "grep", "--quiet", "expected", "HEAD:answer.txt"}}}}
	result, err := Run(context.Background(), input, t.TempDir())
	if err != nil || result.SourceRevision != head || len(result.Checks) != 1 || result.Checks[0].ExitCode != 0 {
		t.Fatalf("verification: %#v %v", result, err)
	}
	input.Checks[0].Command = []string{"git", "grep", "--quiet", "absent", "HEAD:answer.txt"}
	result, err = Run(context.Background(), input, t.TempDir())
	if err != nil || result.Checks[0].ExitCode == 0 {
		t.Fatalf("failed check was lost: %#v %v", result, err)
	}
}

func TestChangesCannotEscapeAuthorizedPathsOrModifyControlFiles(t *testing.T) {
	for _, name := range []string{"../secret", "/etc/passwd", ".git/config", ".github/workflows/build.yml", "AGENTS.md", "cli/../deck/file.go"} {
		if AllowedPath([]string{"."}, name) {
			t.Errorf("unsafe path allowed: %s", name)
		}
	}
	if AllowedPath([]string{"cli"}, "client/main.go") || !AllowedPath([]string{"cli"}, "cli/main.go") {
		t.Fatal("directory scope is not exact")
	}
}

func TestGitReadUsesOneScopedBearerHeader(t *testing.T) {
	seen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = true
		if got := r.Header.Get("Authorization"); got != "Bearer attempt-credential" {
			t.Errorf("Git header=%q", got)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	_, err := Run(context.Background(), Input{Stage: "verify", SourceURL: server.URL, SourceRevision: strings.Repeat("a", 40), GitAuthorization: "attempt-credential"}, t.TempDir())
	if err == nil || !seen {
		t.Fatalf("expected rejected source read, seen=%v err=%v", seen, err)
	}
}

func TestHarnessFailureKeepsFinalStructuredErrorAndRedactsCredential(t *testing.T) {
	output := "raw output must not be copied\n" +
		`{"type":"error","message":"Reconnecting: temporary failure"}` + "\n" +
		`{"type":"turn.failed","error":{"message":"unexpected status 429 with attempt-secret"}}` + "\n"
	if got := harnessFailure(output, "attempt-secret"); got != "unexpected status 429 with [redacted]" {
		t.Fatalf("safe final failure = %q", got)
	}
	if got := harnessFailure(`{"type":"item.completed","item":{"text":"tool output"}}`, "secret"); got != "" {
		t.Fatalf("non-error output exposed: %q", got)
	}
	if got := harnessFailure(`{"type":"error","message":"`+strings.Repeat("x", 1000)+`"}`, "secret"); len(got) > 512 {
		t.Fatalf("unbounded error: %d bytes", len(got))
	}
}
