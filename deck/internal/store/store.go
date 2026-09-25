package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
	_ "modernc.org/sqlite"
)

const defaultProjectID = "prj_default"

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct {
	db *sql.DB

	subMu       sync.RWMutex
	subscribers map[int]chan domain.Event
	nextSubID   int
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is required: %w", domain.ErrInvalidInput)
	}

	if path != ":memory:" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve database path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			return nil, fmt.Errorf("create data directory: %w", err)
		}
		path = absolute
	}

	dsn := sqliteDSN(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	s := &Store{db: db, subscribers: make(map[int]chan domain.Event)}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func sqliteDSN(path string) string {
	var u *url.URL
	if path == ":memory:" {
		u = &url.URL{Scheme: "file", Opaque: "deck-memory"}
		query := u.Query()
		query.Set("mode", "memory")
		query.Set("cache", "shared")
		u.RawQuery = query.Encode()
	} else {
		u = &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	}
	query := u.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "journal_mode(WAL)")
	u.RawQuery = query.Encode()
	return u.String()
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Health verifies that the canonical store can serve requests. It is used by
// readiness surfaces; process liveness deliberately does not depend on it.
func (s *Store) Health(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping Deck store: %w", err)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	// The ledger migration was renamed with the product. Never replay its DDL.
	if _, err := s.db.ExecContext(ctx, `UPDATE schema_migrations
		SET version = '006_deck_ledger.sql' WHERE version = '006_appdeck_ledger.sql'`); err != nil {
		return fmt.Errorf("normalize ledger migration name: %w", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var applied int
		err := s.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", entry.Name(),
		).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", entry.Name(), err)
		}
		if applied > 0 {
			continue
		}

		body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			entry.Name(), nowMillis(),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (s *Store) Subscribe(buffer int) (<-chan domain.Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	s.subMu.Lock()
	id := s.nextSubID
	s.nextSubID++
	ch := make(chan domain.Event, buffer)
	s.subscribers[id] = ch
	s.subMu.Unlock()

	cancel := func() {
		s.subMu.Lock()
		if subscriber, ok := s.subscribers[id]; ok {
			delete(s.subscribers, id)
			close(subscriber)
		}
		s.subMu.Unlock()
	}
	return ch, cancel
}

func (s *Store) publish(event domain.Event) {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	for _, subscriber := range s.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func nowMillis() int64 {
	return time.Now().UTC().UnixMilli()
}

func newID(prefix string) string {
	bytes := make([]byte, 10)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(bytes)
}

func marshalJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func unmarshalMap(value string) map[string]any {
	result := make(map[string]any)
	if value == "" {
		return result
	}
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return map[string]any{"raw": value}
	}
	return result
}

func unmarshalStrings(value string) []string {
	result := make([]string, 0)
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return []string{}
	}
	return result
}

func stringPtr(value string) *string { return &value }

func eventFromRow(scanner interface{ Scan(...any) error }) (domain.Event, error) {
	var event domain.Event
	var taskID, runID, agentID sql.NullString
	var payload string
	if err := scanner.Scan(
		&event.ID,
		&taskID,
		&runID,
		&agentID,
		&event.Type,
		&event.Message,
		&payload,
		&event.CreatedAt,
	); err != nil {
		return domain.Event{}, err
	}
	if taskID.Valid {
		event.TaskID = stringPtr(taskID.String)
	}
	if runID.Valid {
		event.RunID = stringPtr(runID.String)
	}
	if agentID.Valid {
		event.AgentID = stringPtr(agentID.String)
	}
	event.Payload = unmarshalMap(payload)
	return event, nil
}

func insertEventTx(
	ctx context.Context,
	tx *sql.Tx,
	taskID, runID, agentID *string,
	eventType, message string,
	payload map[string]any,
) (domain.Event, error) {
	createdAt := nowMillis()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO events (task_id, run_id, agent_id, type, message, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, taskID, runID, agentID, eventType, message, marshalJSON(payload), createdAt)
	if err != nil {
		return domain.Event{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return domain.Event{}, err
	}
	if err := mirrorTaskEventToThread(ctx, tx, taskID, runID, eventType, message, payload, createdAt); err != nil {
		return domain.Event{}, err
	}
	return domain.Event{
		ID: id, TaskID: taskID, RunID: runID, AgentID: agentID,
		Type: eventType, Message: message, Payload: payload, CreatedAt: createdAt,
	}, nil
}

func normalizeStoreError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}
