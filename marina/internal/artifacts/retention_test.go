package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetentionPreservesReferencesAndRequiresUnchangedReviewedPlan(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)
	put := func(value string, age time.Time) string {
		t.Helper()
		digest, err := store.Put([]byte(value))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(root, strings.TrimPrefix(digest, "sha256:")), age, age); err != nil {
			t.Fatal(err)
		}
		return digest
	}
	retained := put("historical referenced result", old)
	orphan := put("expired orphan", old)
	recent := put("recent unreferenced upload", now)
	refs := map[string]bool{retained: true}
	plan, err := store.PlanRetention(refs, now, 7*24*time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Candidates) != 1 || plan.Candidates[0].Digest != orphan {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := store.Get(orphan); err != nil {
		t.Fatal("dry run removed artifact", err)
	}
	refs[orphan] = true
	if _, err := store.ApplyRetention(plan, refs); err == nil {
		t.Fatal("newly referenced artifact was deleted")
	}
	delete(refs, orphan)
	deleted, err := store.ApplyRetention(plan, refs)
	if err != nil || deleted != 1 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	if _, err := store.Get(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan remains: %v", err)
	}
	for _, digest := range []string{retained, recent} {
		if _, err := store.Get(digest); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRetentionRejectsChangedFilesAndExcludesLinksAndUnknownFiles(t *testing.T) {
	root := t.TempDir()
	store, _ := New(root)
	digest, _ := store.Put([]byte("expired"))
	name := filepath.Join(root, strings.TrimPrefix(digest, "sha256:"))
	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(name, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(name, filepath.Join(root, strings.Repeat("a", 64))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".pending-test"), []byte("pending"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanRetention(nil, now, 24*time.Hour, 1)
	if err != nil || len(plan.Candidates) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if err := os.WriteFile(name, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyRetention(plan, nil); err == nil {
		t.Fatal("changed plan accepted")
	}
	if _, err := store.PlanRetention(nil, now, time.Second, 1); err == nil {
		t.Fatal("unsafe age accepted")
	}
}

func TestMaintenanceLockExcludesControllerAndCollector(t *testing.T) {
	root := t.TempDir()
	lock, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := Lock(root); err == nil {
		other.Close()
		t.Fatal("concurrent maintenance admitted")
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	other, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
}
