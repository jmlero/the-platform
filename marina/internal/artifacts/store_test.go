package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImmutableArtifactSurvivesReopenAndDetectsCorruption(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := store.Put([]byte("verified result"))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	value, err := reopened.Get(digest)
	if err != nil || string(value) != "verified result" {
		t.Fatalf("artifact = %q %v", value, err)
	}
	if _, err := reopened.Get("../../secret"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if err := os.WriteFile(filepath.Join(root, strings.TrimPrefix(digest, "sha256:")), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Get(digest); err == nil {
		t.Fatal("corrupt artifact admitted")
	}
}
