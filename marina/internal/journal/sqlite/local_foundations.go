package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/contextpack"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/policy"
)

func (store *Store) RecordContextPackage(ctx context.Context, manifest contextpack.Package) (contextpack.Package, bool, error) {
	if err := manifest.ValidateAt(store.now()); err != nil {
		return contextpack.Package{}, false, fmt.Errorf("validate context package: %w", err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return contextpack.Package{}, false, fmt.Errorf("encode context package: %w", err)
	}
	result, err := store.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO context_packages (
			digest, execution_id, plan_revision_id, policy_version, source_revision, manifest_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, manifest.Digest, manifest.ExecutionID, manifest.PlanRevisionID, manifest.PolicyVersion,
		manifest.SourceRevision, string(encoded), manifest.CreatedAt.UTC().UnixMilli())
	if err != nil {
		return contextpack.Package{}, false, fmt.Errorf("record context package: %w", err)
	}
	created, _ := result.RowsAffected()
	loaded, err := store.GetContextPackage(ctx, manifest.Digest)
	if err != nil {
		if created == 0 {
			return contextpack.Package{}, false, execution.ErrConflict
		}
		return contextpack.Package{}, false, err
	}
	if loaded.ExecutionID != manifest.ExecutionID || loaded.PlanRevisionID != manifest.PlanRevisionID ||
		loaded.PolicyVersion != manifest.PolicyVersion || loaded.SourceRevision != manifest.SourceRevision {
		return contextpack.Package{}, false, execution.ErrConflict
	}
	return loaded, created == 1, nil
}

func (store *Store) GetContextPackage(ctx context.Context, digest string) (contextpack.Package, error) {
	var encoded string
	if err := store.db.QueryRowContext(ctx,
		"SELECT manifest_json FROM context_packages WHERE digest = ?", strings.TrimSpace(digest),
	).Scan(&encoded); err != nil {
		return contextpack.Package{}, wrapNotFound(err)
	}
	var manifest contextpack.Package
	if err := json.Unmarshal([]byte(encoded), &manifest); err != nil {
		return contextpack.Package{}, fmt.Errorf("decode context package: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return contextpack.Package{}, fmt.Errorf("verify context package: %w", err)
	}
	return manifest, nil
}

// The active one-plan workflow freezes context before submitting its plan.
// Recover this exact package if it was stored before the flow checkpoint.
func (store *Store) ExecutionContextPackage(ctx context.Context, executionID string) (contextpack.Package, error) {
	var digest string
	if err := store.db.QueryRowContext(ctx, "SELECT digest FROM context_packages WHERE execution_id = ? AND plan_revision_id = '' ORDER BY created_at, digest LIMIT 1", executionID).Scan(&digest); err != nil {
		return contextpack.Package{}, wrapNotFound(err)
	}
	return store.GetContextPackage(ctx, digest)
}

func (store *Store) RecordPolicySnapshot(ctx context.Context, snapshot policy.Policy) (policy.Policy, bool, error) {
	if err := snapshot.Validate(); err != nil {
		return policy.Policy{}, false, err
	}
	digest, err := snapshot.Digest()
	if err != nil {
		return policy.Policy{}, false, err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return policy.Policy{}, false, err
	}
	result, err := store.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO policy_snapshots (
			project_id, version, change_class, environment, risk_class, digest, policy_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, snapshot.ProjectID, snapshot.Version, snapshot.ChangeClass, snapshot.Environment,
		snapshot.RiskClass, digest, string(encoded), store.now().UnixMilli())
	if err != nil {
		return policy.Policy{}, false, fmt.Errorf("record policy snapshot: %w", err)
	}
	created, _ := result.RowsAffected()
	loaded, err := store.GetPolicySnapshot(ctx, snapshot.ProjectID, snapshot.Version,
		snapshot.ChangeClass, snapshot.Environment, snapshot.RiskClass)
	if err != nil {
		return policy.Policy{}, false, err
	}
	loadedDigest, err := loaded.Digest()
	if err != nil || loadedDigest != digest {
		return policy.Policy{}, false, execution.ErrConflict
	}
	return loaded, created == 1, nil
}

func (store *Store) GetPolicySnapshot(
	ctx context.Context,
	projectID, version, changeClass, environment string,
	risk policy.RiskClass,
) (policy.Policy, error) {
	var encoded string
	if err := store.db.QueryRowContext(ctx, `
		SELECT policy_json FROM policy_snapshots
		WHERE project_id = ? AND version = ? AND change_class = ? AND environment = ? AND risk_class = ?
	`, projectID, version, changeClass, environment, risk).Scan(&encoded); err != nil {
		return policy.Policy{}, wrapNotFound(err)
	}
	var snapshot policy.Policy
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return policy.Policy{}, fmt.Errorf("decode policy snapshot: %w", err)
	}
	return snapshot, nil
}
