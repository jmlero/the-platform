package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

// Platform is Marina's own work-control ledger (decision 0033). Clients
// create Runs and Turns through marina.dev/v1; the workflow claims, plans,
// verifies and completes them here in-process, with the same idempotency,
// version and fence rules the remote ledger had.
type Platform struct{ store *Store }

func (store *Store) Platform() *Platform { return &Platform{store: store} }

// PlatformError classifies ledger rejections for the reconcilers.
type PlatformError struct {
	Kind    string // conflict, invalid, not_found
	Message string
}

func (e *PlatformError) Error() string          { return e.Message }
func (e *PlatformError) MutationConflict() bool { return e.Kind == "conflict" }
func (e *PlatformError) RetryableFailure() bool { return false }
func (e *PlatformError) Is(target error) bool {
	switch e.Kind {
	case "conflict":
		return target == execution.ErrConflict
	case "not_found":
		return target == execution.ErrNotFound
	case "invalid":
		return target == execution.ErrInvalidInput
	}
	return false
}

func conflict(format string, args ...any) error {
	return &PlatformError{Kind: "conflict", Message: fmt.Sprintf(format, args...)}
}
func invalid(format string, args ...any) error {
	return &PlatformError{Kind: "invalid", Message: fmt.Sprintf(format, args...)}
}
func notFound(what string) error {
	return &PlatformError{Kind: "not_found", Message: what + " not found"}
}

const (
	workReady           = "ready"
	runActiveStatusList = "('planning', 'waiting_decision', 'running', 'verifying')"
)

// PlatformWork is one Run resource as the ledger holds it.
type PlatformWork struct {
	ID, Namespace, Name, UID, ProjectID, SpecDigest string
	Item                                            workcontrol.WorkItem
	Status                                          string
	Version                                         int
	DependsOn                                       []string
	CancelRequested                                 bool
	CreatedAt, UpdatedAt                            time.Time
}

// NewPlatformWork is a Run accepted from a client, already mapped to a frozen
// work item by the controller.
type NewPlatformWork struct {
	Namespace, Name, UID, ProjectID string
	Item                            workcontrol.WorkItem
	DependsOn                       []string
	// Context is the content the client froze into the Run.
	Context workcontrol.ResolvedContext
}

// PlatformWorkID is the ledger identity of a Run resource.
func PlatformWorkID(namespace, name string) string { return namespace + "." + name }

func digestJSON(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func millis(t time.Time) int64 { return t.UTC().UnixMilli() }
func fromMillis(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}

// SubmitWork records a client's Run once. The same name with the same frozen
// item returns the original; a different item is a conflict.
func (p *Platform) SubmitWork(ctx context.Context, input NewPlatformWork) (PlatformWork, bool, error) {
	if input.Namespace == "" || input.Name == "" || input.ProjectID == "" || input.UID == "" {
		return PlatformWork{}, false, invalid("run namespace, name, uid and project are required")
	}
	id := PlatformWorkID(input.Namespace, input.Name)
	item := input.Item
	item.ID, item.ProjectID, item.Status, item.Version = id, input.ProjectID, workReady, 1
	dependsOn := append([]string{}, input.DependsOn...)
	digest := digestJSON(struct {
		Item      workcontrol.WorkItem
		DependsOn []string
		Context   workcontrol.ResolvedContext
	}{workItemWithoutTimes(item), dependsOn, input.Context})
	existing, err := p.Work(ctx, id)
	if err == nil {
		if existing.SpecDigest != digest || existing.UID != input.UID {
			return existing, false, conflict("run %s already exists with a different spec", input.Name)
		}
		return existing, false, nil
	}
	if !errors.Is(err, execution.ErrNotFound) {
		return PlatformWork{}, false, err
	}
	now := p.store.now()
	item.CreatedAt, item.UpdatedAt = now, now
	encoded, _ := json.Marshal(item)
	depends, _ := json.Marshal(dependsOn)
	frozen, _ := json.Marshal(input.Context)
	if _, err := p.store.db.ExecContext(ctx, `INSERT INTO platform_work
		(id, namespace, name, uid, project_id, spec_digest, item_json, status, version, next_fence, depends_json, context_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'ready', 1, 1, ?, ?, ?, ?)`,
		id, input.Namespace, input.Name, input.UID, input.ProjectID, digest, string(encoded), string(depends), string(frozen), millis(now), millis(now)); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return p.SubmitWork(ctx, input)
		}
		return PlatformWork{}, false, err
	}
	if err := p.appendEvent(ctx, p.store.db, id, "run.accepted", "Run accepted", map[string]any{"project": input.ProjectID}, ""); err != nil {
		return PlatformWork{}, false, err
	}
	created, err := p.Work(ctx, id)
	return created, err == nil, err
}

func workItemWithoutTimes(item workcontrol.WorkItem) workcontrol.WorkItem {
	item.CreatedAt, item.UpdatedAt = time.Time{}, time.Time{}
	return item
}

const workColumns = `id, namespace, name, uid, project_id, spec_digest, item_json, status, version, depends_json, cancel_requested, created_at, updated_at`

func scanWork(row scanner) (PlatformWork, error) {
	var w PlatformWork
	var item, depends string
	var cancel int
	var created, updated int64
	if err := row.Scan(&w.ID, &w.Namespace, &w.Name, &w.UID, &w.ProjectID, &w.SpecDigest, &item, &w.Status, &w.Version, &depends, &cancel, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PlatformWork{}, notFound("run")
		}
		return PlatformWork{}, err
	}
	if err := json.Unmarshal([]byte(item), &w.Item); err != nil {
		return PlatformWork{}, err
	}
	_ = json.Unmarshal([]byte(depends), &w.DependsOn)
	w.CancelRequested, w.CreatedAt, w.UpdatedAt = cancel == 1, fromMillis(created), fromMillis(updated)
	w.Item.Status, w.Item.Version, w.Item.UpdatedAt = workcontrol.WorkStatus(w.Status), w.Version, w.UpdatedAt
	return w, nil
}

// Work returns one Run as the ledger holds it.
func (p *Platform) Work(ctx context.Context, id string) (PlatformWork, error) {
	return scanWork(p.store.db.QueryRowContext(ctx, "SELECT "+workColumns+" FROM platform_work WHERE id = ?", id))
}

// ListWork returns Runs ordered by creation, optionally only active ones.
func (p *Platform) ListWork(ctx context.Context, activeOnly bool, limit int) ([]PlatformWork, error) {
	query := "SELECT " + workColumns + " FROM platform_work"
	if activeOnly {
		query += " WHERE status NOT IN ('completed', 'blocked', 'failed', 'cancelled')"
	}
	rows, err := p.store.db.QueryContext(ctx, query+" ORDER BY created_at, id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlatformWork
	for rows.Next() {
		w, err := scanWork(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// mutation runs fn once per idempotency key: an exact replay returns the
// stored result, a changed request with the same key is a conflict.
func (p *Platform) mutation(ctx context.Context, key, operation, target string, request any, result any, fn func(*sql.Tx) (any, error)) error {
	if strings.TrimSpace(key) == "" {
		return invalid("an idempotency key is required")
	}
	requestDigest := digestJSON(request)
	tx, err := p.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var storedOperation, storedTarget, storedDigest, storedResult string
	err = tx.QueryRowContext(ctx, "SELECT operation, target, request_digest, result_json FROM platform_mutations WHERE key = ?", key).
		Scan(&storedOperation, &storedTarget, &storedDigest, &storedResult)
	if err == nil {
		if storedOperation != operation || storedTarget != target || storedDigest != requestDigest {
			return conflict("idempotency key %s was used for a different request", key)
		}
		if result != nil {
			return json.Unmarshal([]byte(storedResult), result)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	value, err := fn(tx)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO platform_mutations (key, operation, target, request_digest, result_json, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		key, operation, target, requestDigest, string(encoded), millis(p.store.now())); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if result != nil {
		return json.Unmarshal(encoded, result)
	}
	return nil
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// appendEvent adds the next client-facing event of a Run or Turn. A repeated
// producer key is ignored, so outbox replays do not duplicate events.
func (p *Platform) appendEvent(ctx context.Context, db execer, owner, eventType, message string, fields map[string]any, producerKey string) error {
	if producerKey != "" {
		var exists int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM platform_events WHERE owner_id = ? AND producer_key = ?", owner, producerKey).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			return nil
		}
	}
	encoded, err := json.Marshal(safeFields(fields))
	if err != nil {
		return err
	}
	if len(message) > 2048 {
		message = message[:2048]
	}
	_, err = db.ExecContext(ctx, `INSERT INTO platform_events (owner_id, sequence, type, message, fields_json, producer_key, occurred_at)
		VALUES (?, (SELECT COALESCE(MAX(sequence), 0) + 1 FROM platform_events WHERE owner_id = ?), ?, ?, ?, ?, ?)`,
		owner, owner, eventType, message, string(encoded), producerKey, millis(p.store.now()))
	return err
}

// safeFields keeps scalar values only: events never carry nested payloads,
// prompts or code.
func safeFields(fields map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range fields {
		switch v := value.(type) {
		case string:
			if len(v) <= 512 {
				out[key] = v
			}
		case bool, int, int64, float64:
			out[key] = v
		}
	}
	return out
}

// PlatformEvent is one ordered event of a Run or Turn.
type PlatformEvent struct {
	Sequence   int64
	Type       string
	Message    string
	Fields     map[string]any
	OccurredAt time.Time
}

func (p *Platform) ListEvents(ctx context.Context, owner string, after int64, limit int) ([]PlatformEvent, error) {
	rows, err := p.store.db.QueryContext(ctx, "SELECT sequence, type, message, fields_json, occurred_at FROM platform_events WHERE owner_id = ? AND sequence > ? ORDER BY sequence LIMIT ?", owner, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlatformEvent
	for rows.Next() {
		var e PlatformEvent
		var fields string
		var at int64
		if err := rows.Scan(&e.Sequence, &e.Type, &e.Message, &fields, &at); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(fields), &e.Fields)
		e.OccurredAt = fromMillis(at)
		out = append(out, e)
	}
	return out, rows.Err()
}
