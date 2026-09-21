package sqlite

import (
	"context"
	"encoding/json"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func (s *Store) CurrentProjectBinding(ctx context.Context, project string) (execution.ProjectBinding, error) {
	return scanBinding(s.db.QueryRowContext(ctx, "SELECT "+bindingColumns+" FROM project_bindings WHERE project_id=?", project))
}
func (s *Store) RecordRepositorySnapshot(ctx context.Context, project, template string, b workcontrol.RepositoryBinding) error {
	if b.Version < 1 || project == "" || template == "" || b.Repository == "" || b.CloneURL == "" || b.BaseBranch == "" {
		return execution.ErrInvalidInput
	}
	encoded, _ := json.Marshal(b)
	_, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO repository_bindings(project_id,version,template_digest,document_json) VALUES (?,?,?,?)", project, b.Version, template, string(encoded))
	if err != nil {
		return err
	}
	old, profile, err := s.RepositorySnapshot(ctx, project, b.Version)
	if err == nil && (old != b || profile != template) {
		return execution.ErrConflict
	}
	return err
}
func (s *Store) RepositorySnapshot(ctx context.Context, project string, version int64) (workcontrol.RepositoryBinding, string, error) {
	var encoded, profile string
	var b workcontrol.RepositoryBinding
	err := s.db.QueryRowContext(ctx, "SELECT document_json,template_digest FROM repository_bindings WHERE project_id=? AND version=?", project, version).Scan(&encoded, &profile)
	if err != nil {
		return b, profile, wrapNotFound(err)
	}
	err = json.Unmarshal([]byte(encoded), &b)
	return b, profile, err
}
func (s *Store) RepositoryReceipt(ctx context.Context, project string, revision int64) (workcontrol.RepositoryResult, error) {
	var encoded string
	var result workcontrol.RepositoryResult
	err := s.db.QueryRowContext(ctx, "SELECT result_json FROM repository_receipts WHERE project_id=? AND request_revision=?", project, revision).Scan(&encoded)
	if err != nil {
		return result, wrapNotFound(err)
	}
	err = json.Unmarshal([]byte(encoded), &result)
	return result, err
}
func (s *Store) RejectRepositoryRequest(ctx context.Context, project string, revision int64, reason string, binding *workcontrol.RepositoryBinding) (workcontrol.RepositoryResult, error) {
	result := workcontrol.RepositoryResult{RequestRevision: revision, Status: "rejected", Reason: reason, Binding: binding}
	encoded, _ := json.Marshal(result)
	_, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO repository_receipts(project_id,request_revision,result_json) VALUES (?,?,?)", project, revision, string(encoded))
	if err != nil {
		return result, err
	}
	return s.RepositoryReceipt(ctx, project, revision)
}
func (s *Store) ApplyRepositoryRequest(ctx context.Context, project string, revision, expected int64, template, configDigest string, b workcontrol.RepositoryBinding) (workcontrol.RepositoryResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workcontrol.RepositoryResult{}, err
	}
	defer tx.Rollback()
	current, err := scanBinding(tx.QueryRowContext(ctx, "SELECT "+bindingColumns+" FROM project_bindings WHERE project_id=?", project))
	if err != nil {
		return workcontrol.RepositoryResult{}, err
	}
	if current.Version != expected {
		return workcontrol.RepositoryResult{}, execution.ErrConflict
	}
	b.Version = current.Version
	if current.Repository != b.CloneURL || current.ContextConfig != configDigest {
		b.Version++
		_, err = tx.ExecContext(ctx, "UPDATE project_bindings SET version=?,repository=?,context_config=?,updated_at=? WHERE project_id=? AND version=?", b.Version, b.CloneURL, configDigest, s.now().UnixMilli(), project, expected)
		if err != nil {
			return workcontrol.RepositoryResult{}, err
		}
	}
	encoded, _ := json.Marshal(b)
	_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO repository_bindings(project_id,version,template_digest,document_json) VALUES (?,?,?,?)", project, b.Version, template, string(encoded))
	if err != nil {
		return workcontrol.RepositoryResult{}, err
	}
	result := workcontrol.RepositoryResult{RequestRevision: revision, Status: "connected", Binding: &b}
	data, _ := json.Marshal(result)
	_, err = tx.ExecContext(ctx, "INSERT INTO repository_receipts(project_id,request_revision,result_json) VALUES (?,?,?)", project, revision, string(data))
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func (s *Store) FirstRepositoryVersion(ctx context.Context, project string) (int64, error) {
	var version int64
	err := s.db.QueryRowContext(ctx, "SELECT MIN(version) FROM repository_bindings WHERE project_id=?", project).Scan(&version)
	return version, err
}
