package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
)

// ArtifactRetentionReferences retains all historical references, including JSON
// nested inside checkpoint strings and unpublished outbox entries. It must be
// called while the artifact maintenance lock excludes the serving controller.
func (store *Store) ArtifactRetentionReferences(ctx context.Context, directory string) (map[string]bool, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_bindings WHERE artifact_config = ?`, absolute).Scan(&count); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, errors.New("artifact directory does not match a journal project binding")
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM executions WHERE state NOT IN ('completed','blocked','failed','cancelled')`).Scan(&count); err != nil {
		return nil, err
	}
	if count != 0 {
		return nil, errors.New("active executions remain; finish or cancel work before artifact maintenance")
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversation_turns WHERE status IN ('pending','running')`).Scan(&count); err != nil {
		return nil, err
	}
	if count != 0 {
		return nil, errors.New("active conversation turns remain; finish work before artifact maintenance")
	}
	rows, err := store.db.QueryContext(ctx, `
		SELECT digest FROM runtime_artifacts UNION ALL SELECT uri FROM runtime_artifacts
		UNION ALL SELECT payload_json FROM checkpoints
		UNION ALL SELECT uri FROM evidence
		UNION ALL SELECT output_refs_json FROM runtime_attempts
		UNION ALL SELECT specification_json FROM runtime_attempts
		UNION ALL SELECT manifest_json FROM context_packages
		UNION ALL SELECT payload_json FROM provider_outbox
		UNION ALL SELECT payload_json FROM events
		UNION ALL SELECT specification_json FROM conversation_turns
		UNION ALL SELECT result_digest FROM conversation_turns
		UNION ALL SELECT publication_json FROM conversation_turns
		UNION ALL SELECT digest FROM conversation_artifacts
		UNION ALL SELECT uri FROM conversation_artifacts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Match hashes in both sha256: references and artifact URL path segments.
	pattern := regexp.MustCompile(`[0-9a-f]{64}`)
	references := map[string]bool{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		for _, digest := range pattern.FindAllString(value, -1) {
			references["sha256:"+digest] = true
		}
	}
	return references, rows.Err()
}
