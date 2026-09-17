package artifacts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"golang.org/x/sys/unix"
)

// Lock excludes the serving controller and other collectors for the entire
// read-plan-delete operation, including the gap between upload and journaling.
// Callers retain this lock until all artifact and journal access has stopped.
func Lock(root string) (*os.File, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(root, ".maintenance.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "artifact maintenance lock")
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("artifact store is in use; stop Marina before maintenance")
	}
	return file, nil
}

type RetentionCandidate struct {
	Digest     string    `json:"digest"`
	Bytes      int64     `json:"bytes"`
	ModifiedAt time.Time `json:"modified_at"`
}

type RetentionPlan struct {
	Version           string               `json:"version"`
	Directory         string               `json:"directory"`
	CreatedAt         time.Time            `json:"created_at"`
	MinimumAgeSeconds int64                `json:"minimum_age_seconds"`
	Limit             int                  `json:"limit"`
	Candidates        []RetentionCandidate `json:"candidates"`
}

// PlanRetention never changes artifacts. All journal references, regardless of
// task state or age, are protected. Unknown files and symlinks are left alone.
func (store *Store) PlanRetention(references map[string]bool, now time.Time, age time.Duration, limit int) (RetentionPlan, error) {
	plan := RetentionPlan{Version: "marina-artifact-retention/v1", Directory: store.root,
		CreatedAt: now.UTC(), MinimumAgeSeconds: int64(age / time.Second), Limit: limit,
		Candidates: []RetentionCandidate{}}
	if age < 24*time.Hour || age > 3650*24*time.Hour || age%time.Second != 0 || limit < 1 || limit > 1000 || now.IsZero() || now.After(time.Now().Add(time.Minute)) {
		return plan, errors.New("retention requires age 24h–87600h, limit 1–1000, and a non-future observation time")
	}
	entries, err := os.ReadDir(store.root)
	if err != nil {
		return plan, err
	}
	for _, entry := range entries {
		digest := "sha256:" + entry.Name()
		if _, err := store.name(digest); err != nil || references[digest] || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return plan, err
		}
		if !info.ModTime().Before(now.Add(-age)) {
			continue
		}
		if _, err := store.Get(digest); err != nil {
			return plan, fmt.Errorf("retention candidate failed integrity check: %w", err)
		}
		plan.Candidates = append(plan.Candidates, RetentionCandidate{Digest: digest, Bytes: info.Size(), ModifiedAt: info.ModTime().UTC()})
		if len(plan.Candidates) == limit {
			break
		}
	}
	return plan, nil
}

// ApplyRetention requires a reviewed dry-run plan and a fresh reference set.
// The caller must hold Lock and prevent all writers until this returns.
func (store *Store) ApplyRetention(plan RetentionPlan, references map[string]bool) (int, error) {
	if plan.Version != "marina-artifact-retention/v1" || plan.Directory != store.root ||
		plan.MinimumAgeSeconds < 86400 || plan.MinimumAgeSeconds > 3650*86400 ||
		time.Since(plan.CreatedAt) > 24*time.Hour {
		return 0, errors.New("invalid, mismatched or expired retention plan; create a fresh dry run")
	}
	fresh, err := store.PlanRetention(references, plan.CreatedAt, time.Duration(plan.MinimumAgeSeconds)*time.Second, plan.Limit)
	if err != nil {
		return 0, err
	}
	if !reflect.DeepEqual(fresh, plan) {
		return 0, errors.New("retention candidates changed; create and review a fresh dry run")
	}
	for index, candidate := range plan.Candidates {
		name, _ := store.name(candidate.Digest)
		if err := os.Remove(name); err != nil {
			return index, err
		}
	}
	directory, err := os.Open(store.root)
	if err != nil {
		return len(plan.Candidates), err
	}
	defer directory.Close()
	return len(plan.Candidates), directory.Sync()
}
