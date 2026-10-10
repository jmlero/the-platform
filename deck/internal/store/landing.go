package store

import (
	"encoding/hex"
	"fmt"
	"github.com/jmlero/the-platform/deck/internal/domain"
	"slices"
	"strings"
)

func validateLanding(task domain.Task, landing domain.LandingObservation, items []domain.EvidenceRecord, now int64) error {
	commit, err := hex.DecodeString(landing.Commit)
	if err != nil || (len(commit) != 20 && len(commit) != 32) || strings.ToLower(landing.Commit) != landing.Commit || landing.ObservedAt <= 0 || landing.ObservedAt > now+60_000 || now-landing.ObservedAt > 24*60*60*1000 {
		return fmt.Errorf("landing requires a recent exact-commit observation: %w", domain.ErrInvalidInput)
	}
	if !slices.Contains(task.Budget.Authority, "source.fast_forward") || task.Context == nil || task.Context.Repository == nil || task.Context.Repository.Repository != landing.Repository || task.Context.Repository.BaseBranch != landing.BaseBranch {
		return fmt.Errorf("landing is outside the authorized repository and base branch: %w", domain.ErrConflict)
	}
	kinds := map[string]bool{}
	for _, item := range items {
		if item.SourceRevision != landing.Commit || item.Outcome != "passed" || item.AdmissionStatus != "current" || item.Producer == item.Verifier {
			return fmt.Errorf("landing evidence must independently pass on the landed commit: %w", domain.ErrConflict)
		}
		kinds[item.Type] = true
	}
	if !kinds["landing"] || !kinds["test"] || !kinds["semantic_review"] {
		return fmt.Errorf("landing requires source/CI, test and independent-review evidence: %w", domain.ErrConflict)
	}
	return nil
}
