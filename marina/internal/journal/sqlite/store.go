package sqlite

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
	"sync/atomic"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var memoryDatabaseSequence atomic.Uint64

type options struct {
	clock func() time.Time
}

type Option func(*options)

func WithClock(clock func() time.Time) Option {
	return func(settings *options) {
		if clock != nil {
			settings.clock = clock
		}
	}
}

type Store struct {
	db    *sql.DB
	clock func() time.Time
}

var _ execution.Journal = (*Store)(nil)

func Open(ctx context.Context, path string, optionValues ...Option) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("database path is required: %w", execution.ErrInvalidInput)
	}

	settings := options{clock: time.Now}
	for _, option := range optionValues {
		option(&settings)
	}

	if path != ":memory:" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve database path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
		path = absolute
	}

	database, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite journal: %w", err)
	}
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(2)
	database.SetConnMaxLifetime(0)

	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("ping sqlite journal: %w", err)
	}

	store := &Store{db: database, clock: settings.clock}
	if err := store.migrate(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) Close() error {
	return store.db.Close()
}

func (store *Store) Health(ctx context.Context) error {
	if err := store.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite journal: %w", err)
	}
	var result int
	if err := store.db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		return fmt.Errorf("query sqlite journal: %w", err)
	}
	if result != 1 {
		return errors.New("sqlite journal health query returned an unexpected result")
	}
	return nil
}

func sqliteDSN(path string) string {
	var parsed *url.URL
	if path == ":memory:" {
		name := fmt.Sprintf("marina-memory-%d", memoryDatabaseSequence.Add(1))
		parsed = &url.URL{Scheme: "file", Opaque: name}
		query := parsed.Query()
		query.Set("mode", "memory")
		query.Set("cache", "shared")
		parsed.RawQuery = query.Encode()
	} else {
		parsed = &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	}
	query := parsed.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "journal_mode(WAL)")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (store *Store) migrate(ctx context.Context) error {
	if _, err := store.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read journal migrations: %w", err)
	}
	sort.Slice(entries, func(left int, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var applied int
		if err := store.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", entry.Name(),
		).Scan(&applied); err != nil {
			return fmt.Errorf("check journal migration %s: %w", entry.Name(), err)
		}
		if applied > 0 {
			continue
		}

		body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read journal migration %s: %w", entry.Name(), err)
		}
		transaction, err := store.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin journal migration %s: %w", entry.Name(), err)
		}
		if _, err := transaction.ExecContext(ctx, string(body)); err != nil {
			_ = transaction.Rollback()
			return fmt.Errorf("apply journal migration %s: %w", entry.Name(), err)
		}
		if _, err := transaction.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			entry.Name(), store.now().UnixMilli(),
		); err != nil {
			_ = transaction.Rollback()
			return fmt.Errorf("record journal migration %s: %w", entry.Name(), err)
		}
		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("commit journal migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (store *Store) now() time.Time {
	return store.clock().UTC()
}

func newID(prefix string) (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate %s ID: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(value), nil
}

func marshalPayload(payload map[string]any) (string, error) {
	if payload == nil {
		return "{}", nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode payload: %w", err)
	}
	return string(encoded), nil
}

func unmarshalPayload(encoded string) map[string]any {
	payload := make(map[string]any)
	if encoded == "" {
		return payload
	}
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		return map[string]any{"unreadable": true}
	}
	return payload
}

func insertEvent(
	ctx context.Context,
	transaction *sql.Tx,
	executionID string,
	eventType string,
	message string,
	payload map[string]any,
	createdAt time.Time,
) error {
	encoded, err := marshalPayload(payload)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO events (execution_id, type, message, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, executionID, eventType, message, encoded, createdAt.UnixMilli()); err != nil {
		return fmt.Errorf("insert execution event: %w", err)
	}
	return nil
}

func wrapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return execution.ErrNotFound
	}
	return err
}
