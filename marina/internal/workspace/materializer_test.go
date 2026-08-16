package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializerChecksOutAndVerifiesExactPinnedRevision(t *testing.T) {
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source")
	runGit(t, ctx, "", "init", source)
	runGit(t, ctx, source, "config", "user.email", "marina-test@example.invalid")
	runGit(t, ctx, source, "config", "user.name", "Marina Test")
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("pinned content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, ctx, source, "add", "README.md")
	runGit(t, ctx, source, "commit", "-m", "fixture")
	revision := strings.TrimSpace(runGit(t, ctx, source, "rev-parse", "HEAD"))

	root := filepath.Join(t.TempDir(), "workspaces")
	materializer, err := NewMaterializer(root, "git")
	if err != nil {
		t.Fatal(err)
	}
	request := Request{ExecutionID: "exec-1", AttemptID: "attempt-1", Repository: source, Revision: revision}
	result, err := materializer.Materialize(ctx, request)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if result.Revision != revision || !strings.HasPrefix(result.Path, root+string(filepath.Separator)) {
		t.Fatalf("materialized result = %#v", result)
	}
	content, err := os.ReadFile(filepath.Join(result.Path, "README.md"))
	if err != nil || string(content) != "pinned content\n" {
		t.Fatalf("workspace content = %q, err %v", content, err)
	}
	replayed, err := materializer.Materialize(ctx, request)
	if err != nil || replayed.Path != result.Path || replayed.Revision != result.Revision {
		t.Fatalf("replayed materialization = %#v, err %v", replayed, err)
	}

	bad := request
	bad.AttemptID = "attempt-bad"
	bad.Revision = strings.Repeat("0", len(revision))
	if _, err := materializer.Materialize(ctx, bad); err == nil {
		t.Fatal("missing pinned revision was accepted")
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
