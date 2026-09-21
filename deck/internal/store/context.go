package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const maxContextSources = 32
const maxContextMessages = 20
const maxContextBytes = 128 << 10

var contextIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$`)
var gitRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func contextHash(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func validateContextSource(input domain.ContextSourceInput) (domain.ContextSourceInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	if input.Trust == "" {
		input.Trust = "data"
	}
	if input.State == "" {
		input.State = "active"
	}
	invalid := func(message string) (domain.ContextSourceInput, error) {
		return input, fmt.Errorf("%s: %w", message, domain.ErrInvalidInput)
	}
	if !contextIDPattern.MatchString(input.ID) || input.Title == "" || len(input.Title) > 200 || input.ExpectedRevision < 0 || len(input.Content) > 16<<10 || len(input.ConflictsWith) > maxContextSources || input.ExpiresAt < 0 {
		return invalid("invalid or oversized context source")
	}
	if input.Trust != "guidance" && input.Trust != "data" {
		return invalid("context trust must be guidance or data")
	}
	if input.State != "active" && input.State != "inactive" {
		return invalid("context source state must be active or inactive")
	}
	for _, id := range input.ConflictsWith {
		if !contextIDPattern.MatchString(id) || id == input.ID {
			return invalid("invalid conflicting source ID")
		}
	}
	switch input.Kind {
	case "instruction", "goal", "document":
		if strings.TrimSpace(input.Content) == "" || input.Reference != "" || input.SourceRevision != "" || input.Path != "" {
			return invalid("authored context needs content and no external reference")
		}
	case "git":
		if input.Content != "" || !gitRevisionPattern.MatchString(input.SourceRevision) || input.Reference != "" || input.Path == "" || input.Path == "." || input.Path == ".." || len(input.Path) > 512 || path.IsAbs(input.Path) || path.Clean(input.Path) != input.Path || strings.HasPrefix(input.Path, "../") || strings.ContainsAny(input.Path, "\\\x00\r\n") {
			return invalid("Git sources need an exact commit and a repository-relative file path; content stays in Git")
		}
	case "decision":
		if input.Reference == "" || input.SourceRevision == "" || input.Content != "" || input.Path != "" {
			return invalid("decision sources need a canonical decision ID and revision")
		}
	default:
		return invalid("unsupported context source kind")
	}
	return input, nil
}

func (s *Store) PutContextSource(ctx context.Context, projectID string, input domain.ContextSourceInput) (domain.ContextSource, error) {
	input, err := validateContextSource(input)
	if err != nil {
		return domain.ContextSource{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ContextSource{}, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE id = ?", projectID).Scan(&exists); err != nil {
		return domain.ContextSource{}, err
	}
	if exists != 1 {
		return domain.ContextSource{}, domain.ErrNotFound
	}
	var latest int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(revision), 0) FROM context_sources WHERE project_id = ? AND id = ?", projectID, input.ID).Scan(&latest); err != nil {
		return domain.ContextSource{}, err
	}
	if latest != input.ExpectedRevision {
		if latest == input.ExpectedRevision+1 {
			var encoded string
			var previous domain.ContextSource
			var revoked int64
			if err = tx.QueryRowContext(ctx, "SELECT document_json,revoked_at FROM context_sources WHERE project_id=? AND id=? AND revision=?", projectID, input.ID, latest).Scan(&encoded, &revoked); err != nil {
				return previous, err
			}
			if err = json.Unmarshal([]byte(encoded), &previous); err != nil {
				return previous, err
			}
			input.ExpectedRevision = 0
			if marshalJSON(previous.ContextSourceInput) == marshalJSON(input) {
				previous.RevokedAt = revoked
				return previous, nil
			}
		}
		return domain.ContextSource{}, domain.ErrConflict
	}
	if input.Kind == "decision" {
		var revision string
		if err = tx.QueryRowContext(ctx, `SELECT d.plan_revision_id FROM decisions d JOIN tasks t ON t.id = d.task_id WHERE d.id = ? AND t.project_id = ? AND d.status = 'answered' AND d.answer = 'Approve plan'`, input.Reference, projectID).Scan(&revision); err != nil || revision != input.SourceRevision {
			return domain.ContextSource{}, fmt.Errorf("select an approved decision and its exact plan revision in this project: %w", domain.ErrInvalidInput)
		}
	}
	input.ExpectedRevision = 0
	source := domain.ContextSource{ContextSourceInput: input, ProjectID: projectID, Revision: latest + 1, CreatedAt: nowMillis()}
	source.Digest = contextHash(source)
	if _, err = tx.ExecContext(ctx, "INSERT INTO context_sources(project_id, id, revision, document_json) VALUES (?, ?, ?, ?)", projectID, input.ID, source.Revision, marshalJSON(source)); err != nil {
		return domain.ContextSource{}, err
	}
	event, err := insertEventTx(ctx, tx, nil, nil, nil, "context.source.revised", "Project context source revised", map[string]any{"project_id": projectID, "source_id": source.ID, "revision": source.Revision})
	if err != nil {
		return domain.ContextSource{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.ContextSource{}, err
	}
	s.publish(event)
	return source, nil
}

type contextQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func contextSources(ctx context.Context, q contextQuerier, projectID string, history bool) ([]domain.ContextSource, error) {
	query := `SELECT document_json, revoked_at, revision < (SELECT MAX(other.revision) FROM context_sources other WHERE other.project_id = c.project_id AND other.id = c.id) FROM context_sources c WHERE project_id = ?`
	if !history {
		query += ` AND revision = (SELECT MAX(other.revision) FROM context_sources other WHERE other.project_id = c.project_id AND other.id = c.id)`
	}
	query += " ORDER BY id, revision"
	rows, err := q.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.ContextSource{}
	for rows.Next() {
		var encoded string
		var revoked int64
		var superseded bool
		if err = rows.Scan(&encoded, &revoked, &superseded); err != nil {
			return nil, err
		}
		var item domain.ContextSource
		if err = json.Unmarshal([]byte(encoded), &item); err != nil {
			return nil, err
		}
		item.RevokedAt, item.Superseded = revoked, superseded
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListContextSources(ctx context.Context, projectID string) ([]domain.ContextSource, error) {
	return contextSources(ctx, s.db, projectID, true)
}

func (s *Store) RevokeContextSource(ctx context.Context, projectID, id string, revision int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revoked int64
	if err = tx.QueryRowContext(ctx, "SELECT revoked_at FROM context_sources WHERE project_id=? AND id=? AND revision=?", projectID, id, revision).Scan(&revoked); err != nil {
		return normalizeStoreError(err)
	}
	if revoked != 0 {
		return nil
	}
	result, err := tx.ExecContext(ctx, "UPDATE context_sources SET revoked_at = CASE WHEN revoked_at = 0 THEN ? ELSE revoked_at END WHERE project_id = ? AND id = ? AND revision = ?", nowMillis(), projectID, id, revision)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return domain.ErrNotFound
	}
	event, err := insertEventTx(ctx, tx, nil, nil, nil, "context.source.revoked", "Project context source revoked", map[string]any{"project_id": projectID, "source_id": id, "revision": revision})
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.publish(event)
	return nil
}

func selectTaskContext(ctx context.Context, tx *sql.Tx, projectID string, messageIDs []string, authorized bool) (*domain.TaskContext, error) {
	if len(messageIDs) > maxContextMessages {
		return nil, fmt.Errorf("select at most %d messages: %w", maxContextMessages, domain.ErrInvalidInput)
	}
	selection := &domain.TaskContext{Sources: []domain.ContextReference{}, Messages: []domain.ContextReference{}}
	if authorized {
		selection.AuthorizedAt = nowMillis()
		repository, err := getRepository(ctx, tx, projectID)
		if err != nil {
			return nil, err
		}
		selection.Repository = repository.Binding
		sources, err := contextSources(ctx, tx, projectID, false)
		if err != nil {
			return nil, err
		}
		for _, item := range sources {
			if item.State != "active" || item.RevokedAt != 0 {
				continue
			}
			selection.Sources = append(selection.Sources, domain.ContextReference{ID: item.ID, Revision: item.Revision, Digest: item.Digest})
		}
		if len(selection.Sources) > maxContextSources {
			return nil, fmt.Errorf("select at most %d project sources: %w", maxContextSources, domain.ErrInvalidInput)
		}
	}
	seen := map[string]bool{}
	for _, id := range messageIDs {
		if seen[id] {
			return nil, fmt.Errorf("duplicate context message: %w", domain.ErrInvalidInput)
		}
		seen[id] = true
		message, err := contextMessage(ctx, tx, projectID, id)
		if err != nil {
			return nil, err
		}
		selection.Messages = append(selection.Messages, domain.ContextReference{ID: id, Revision: 1, Digest: message.Digest})
	}
	if len(selection.Sources) == 0 && len(selection.Messages) == 0 && selection.Repository == nil {
		return nil, nil
	}
	resolved, err := resolveContext(ctx, tx, projectID, selection)
	if err != nil {
		return nil, err
	}
	if len(marshalJSON(resolved)) > maxContextBytes {
		return nil, fmt.Errorf("selected context exceeds 128 KiB: %w", domain.ErrInvalidInput)
	}
	return selection, nil
}

func contextMessage(ctx context.Context, q contextQuerier, projectID, id string) (domain.ContextMessage, error) {
	var item domain.ContextMessage
	if err := q.QueryRowContext(ctx, `SELECT id, channel_id, author_type, author_id, body, created_at FROM channel_messages WHERE id = ? AND project_id = ?`, id, projectID).Scan(&item.ID, &item.ChannelID, &item.AuthorType, &item.AuthorID, &item.Content, &item.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return item, fmt.Errorf("selected message is not in this project: %w", domain.ErrInvalidInput)
		}
		return item, err
	}
	item.Trust = "data"
	item.Digest = contextHash(item)
	return item, nil
}

func resolveContext(ctx context.Context, q contextQuerier, projectID string, selection *domain.TaskContext) (domain.ResolvedContext, error) {
	result := domain.ResolvedContext{Selection: selection, Sources: []domain.ContextSource{}, Messages: []domain.ContextMessage{}}
	if selection == nil {
		return result, nil
	}
	for _, ref := range selection.Sources {
		var encoded string
		var revoked int64
		if err := q.QueryRowContext(ctx, "SELECT document_json, revoked_at FROM context_sources WHERE project_id = ? AND id = ? AND revision = ?", projectID, ref.ID, ref.Revision).Scan(&encoded, &revoked); err != nil {
			return result, normalizeStoreError(err)
		}
		var source domain.ContextSource
		if err := json.Unmarshal([]byte(encoded), &source); err != nil {
			return result, err
		}
		if source.Digest != ref.Digest {
			return result, domain.ErrConflict
		}
		source.RevokedAt = revoked
		// Canonical decisions stay in their existing ledger. Materialize only the
		// selected immutable revision for the consumer's derived snapshot.
		if source.Kind == "decision" {
			if err := q.QueryRowContext(ctx, `SELECT d.question || char(10) || d.answer || char(10) || p.plan FROM decisions d JOIN plan_revisions p ON p.id = d.plan_revision_id JOIN tasks t ON t.id = d.task_id WHERE d.id = ? AND d.plan_revision_id = ? AND t.project_id = ? AND d.status = 'answered' AND d.answer = 'Approve plan'`, source.Reference, source.SourceRevision, projectID).Scan(&source.Content); err != nil {
				return result, fmt.Errorf("approved decision is unavailable: %w", domain.ErrConflict)
			}
		}
		result.Sources = append(result.Sources, source)
	}
	for _, ref := range selection.Messages {
		message, err := contextMessage(ctx, q, projectID, ref.ID)
		if err != nil {
			return result, err
		}
		if ref.Revision != 1 || ref.Digest != message.Digest {
			return result, domain.ErrConflict
		}
		result.Messages = append(result.Messages, message)
	}
	return result, nil
}

func (s *Store) ResolveTaskContext(ctx context.Context, taskID string) (domain.ResolvedContext, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return domain.ResolvedContext{}, err
	}
	return resolveContext(ctx, s.db, task.ProjectID, task.Context)
}

func (s *Store) ResolveRunContext(ctx context.Context, runID string) (domain.ResolvedContext, error) {
	var projectID, encoded string
	if err := s.db.QueryRowContext(ctx, `SELECT t.project_id, COALESCE(c.context_json, 'null') FROM runs r JOIN tasks t ON t.id = r.task_id LEFT JOIN run_context c ON c.run_id = r.id WHERE r.id = ?`, runID).Scan(&projectID, &encoded); err != nil {
		return domain.ResolvedContext{}, normalizeStoreError(err)
	}
	var selection *domain.TaskContext
	if err := json.Unmarshal([]byte(encoded), &selection); err != nil {
		return domain.ResolvedContext{}, err
	}
	result, err := resolveContext(ctx, s.db, projectID, selection)
	if err != nil {
		return result, err
	}
	var payload string
	err = s.db.QueryRowContext(ctx, "SELECT payload_json FROM events WHERE run_id=? AND type='context.assembled' ORDER BY id LIMIT 1", runID).Scan(&payload)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal([]byte(payload), &result.Assembly); err != nil {
		return result, err
	}
	return result, nil
}

func selectedMessageIDs(selection *domain.TaskContext) []string {
	var ids []string
	if selection != nil {
		for _, message := range selection.Messages {
			ids = append(ids, message.ID)
		}
	}
	return ids
}
