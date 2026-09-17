package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
)

func runArtifactRetention(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("artifact-retention", flag.ContinueOnError)
	flags.SetOutput(stderr)
	journalPath := flags.String("journal-path", "", "existing Marina journal; required")
	directory := flags.String("artifact-directory", "", "existing bound artifact store; required")
	age := flags.Duration("min-age", 168*time.Hour, "minimum unreferenced age, at least 24h")
	limit := flags.Int("limit", 100, "maximum artifacts in this batch, at most 1000")
	apply := flags.String("apply-plan", "", "reviewed dry-run JSON file; absent only reports candidates")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *journalPath == "" || *directory == "" {
		fmt.Fprintln(stderr, "existing --journal-path and --artifact-directory are required; stop Marina before maintenance")
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "artifact retention:", err); return 1 }
	for _, path := range []string{*journalPath, *directory} {
		if _, err := os.Stat(path); err != nil {
			return fail(err)
		}
	}
	lock, err := artifacts.Lock(*directory)
	if err != nil {
		return fail(err)
	}
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	journal, err := sqlite.Open(ctx, *journalPath)
	if err != nil {
		return fail(err)
	}
	defer journal.Close()
	refs, err := journal.ArtifactRetentionReferences(ctx, *directory)
	if err != nil {
		return fail(err)
	}
	store, err := artifacts.New(*directory)
	if err != nil {
		return fail(err)
	}
	if *apply == "" {
		plan, err := store.PlanRetention(refs, time.Now(), *age, *limit)
		if err != nil {
			return fail(err)
		}
		if err := json.NewEncoder(stdout).Encode(plan); err != nil {
			return fail(err)
		}
		return 0
	}
	file, err := os.Open(*apply)
	if err != nil {
		return fail(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var plan artifacts.RetentionPlan
	if err := decoder.Decode(&plan); err != nil {
		return fail(err)
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return fail(fmt.Errorf("one retention plan document required"))
	}
	deleted, err := store.ApplyRetention(plan, refs)
	if writeErr := json.NewEncoder(stdout).Encode(map[string]int{"deleted": deleted}); writeErr != nil {
		return fail(writeErr)
	}
	if err != nil {
		return fail(err)
	}
	return 0
}
