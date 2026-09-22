package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jmlero/the-platform/deck/internal/domain"
	"slices"
	"strings"
	"time"
)

func (s *Store) PublishWorkloads(ctx context.Context, project, owner string, catalog domain.WorkloadCatalog) error {
	if owner == "" || len(catalog.Profiles) > 2 || catalog.ExpiresAt <= nowMillis() || catalog.ExpiresAt > time.Now().Add(5*time.Minute).UnixMilli() {
		return domain.ErrInvalidInput
	}
	seen := map[string]bool{}
	for _, p := range catalog.Profiles {
		digest, err := hex.DecodeString(p.Digest)
		if err != nil || len(digest) != 32 || p.Digest != strings.ToLower(p.Digest) || seen[p.ID] || p.MaxTokens < 1 || p.BudgetMinutes < 1 || p.MinAttempts < 1 {
			return domain.ErrInvalidInput
		}
		seen[p.ID] = true
		switch p.ID {
		case "diagnostic-analysis/v1":
			if (p.Harness != "langchain-create-agent/v1" && p.Harness != "langchain-create-agent/v2") || !slices.Equal(p.Authority, []string{"analysis.run." + p.Digest}) || p.MinAttempts != 1 {
				return domain.ErrInvalidInput
			}
		case "repository/v1":
			if p.Harness != "codex/v1" || !slices.Equal(p.Authority, []string{"source.read", "source.write", "pull_request.create"}) || p.MinAttempts != 2 {
				return domain.ErrInvalidInput
			}
		default:
			return domain.ErrInvalidInput
		}
		if p.ID == "diagnostic-analysis/v1" && p.Harness == "langchain-create-agent/v1" && (len(p.Tools) > 0 || len(p.ContextKinds) > 0) {
			return domain.ErrInvalidInput
		}
		if len(p.Tools) > 1 {
			return domain.ErrInvalidInput
		}
		for _, tool := range p.Tools {
			if tool != "read_context_source" {
				return domain.ErrInvalidInput
			}
		}
		for _, kind := range p.ContextKinds {
			if !slices.Contains([]string{"instruction", "goal", "document", "decision", "git", "message"}, kind) {
				return domain.ErrInvalidInput
			}
		}
	}
	if catalog.Profiles == nil {
		catalog.Profiles = []domain.WorkloadProfile{}
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO workload_catalogs(project_id,owner_id,catalog_json) VALUES(?,?,?) ON CONFLICT(project_id) DO UPDATE SET catalog_json=excluded.catalog_json WHERE owner_id=excluded.owner_id`, project, owner, marshalJSON(catalog))
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return domain.ErrConflict
	}
	return nil
}
func (s *Store) GetWorkloads(ctx context.Context, project string) (domain.WorkloadCatalog, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT catalog_json FROM workload_catalogs WHERE project_id=?", project).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkloadCatalog{Profiles: []domain.WorkloadProfile{}}, nil
	}
	var c domain.WorkloadCatalog
	if err == nil {
		err = json.Unmarshal([]byte(raw), &c)
	}
	if c.ExpiresAt <= nowMillis() {
		c.Profiles = []domain.WorkloadProfile{}
	}
	return c, err
}
func admittedProfile(ctx context.Context, tx *sql.Tx, project string, in domain.WorkloadInvocation, b domain.Budget) (*domain.WorkloadProfile, error) {
	var raw string
	if err := tx.QueryRowContext(ctx, "SELECT catalog_json FROM workload_catalogs WHERE project_id=?", project).Scan(&raw); err != nil {
		return nil, fmt.Errorf("no installed workload profile: %w", domain.ErrConflict)
	}
	var catalog domain.WorkloadCatalog
	if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
		return nil, err
	}
	if catalog.ExpiresAt <= nowMillis() {
		return nil, fmt.Errorf("workload catalog expired: %w", domain.ErrConflict)
	}
	for _, p := range catalog.Profiles {
		if p.ID != in.ProfileID || p.Digest != in.ProfileDigest {
			continue
		}
		if strings.TrimSpace(in.Task.AcceptanceCriteria) == "" || b.MaxTokens < 1 || b.MaxTokens > p.MaxTokens || b.TimeMinutes > p.BudgetMinutes || b.MaxConcurrency != 1 || b.MaxAttempts < p.MinAttempts || !sameGrants(b.Authority, p.Authority) || (b.DeadlineAt != nil && *b.DeadlineAt <= nowMillis()) {
			return nil, fmt.Errorf("explicit criteria, authority and budgets within the selected profile are required: %w", domain.ErrInvalidInput)
		}
		return &p, nil
	}
	return nil, fmt.Errorf("profile unavailable or changed: %w", domain.ErrConflict)
}
func sameGrants(a, b []string) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
func (s *Store) InvokeWorkload(ctx context.Context, project string, mutation Mutation, in domain.WorkloadInvocation) (domain.Task, error) {
	if in.Task.ProjectID != "" && in.Task.ProjectID != project || in.Task.Status != "" && in.Task.Status != domain.TaskReady || in.Task.Budget == nil || in.Task.Budget.TimeMinutes < 1 {
		return domain.Task{}, domain.ErrInvalidInput
	}
	input := in.Task
	input.ProjectID = project
	input.Status = domain.TaskReady
	return s.createTask(ctx, input, &in, mutation)
}
