package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

type OperatorSession struct {
	TokenHash  string
	CSRFHash   string
	OperatorID string
	ExpiresAt  int64
	CreatedAt  int64
}

func (s *Store) CreateOperatorSession(ctx context.Context, session OperatorSession) error {
	session.TokenHash = strings.ToLower(strings.TrimSpace(session.TokenHash))
	session.CSRFHash = strings.ToLower(strings.TrimSpace(session.CSRFHash))
	session.OperatorID = strings.TrimSpace(session.OperatorID)
	if !validSHA256Digest(session.TokenHash) || !validSHA256Digest(session.CSRFHash) ||
		session.OperatorID == "" || len(session.OperatorID) > 128 || session.ExpiresAt <= nowMillis() {
		return fmt.Errorf("invalid operator session: %w", domain.ErrInvalidInput)
	}
	session.CreatedAt = nowMillis()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin operator session: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM operator_sessions WHERE expires_at <= ?", session.CreatedAt); err != nil {
		return fmt.Errorf("purge expired operator sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO operator_sessions (token_hash, csrf_hash, operator_id, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, session.TokenHash, session.CSRFHash, session.OperatorID, session.ExpiresAt, session.CreatedAt); err != nil {
		return fmt.Errorf("create operator session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit operator session: %w", err)
	}
	return nil
}

func (s *Store) GetOperatorSession(ctx context.Context, tokenHash string, now int64) (OperatorSession, error) {
	tokenHash = strings.ToLower(strings.TrimSpace(tokenHash))
	if !validSHA256Digest(tokenHash) {
		return OperatorSession{}, domain.ErrNotFound
	}
	var session OperatorSession
	err := s.db.QueryRowContext(ctx, `
		SELECT token_hash, csrf_hash, operator_id, expires_at, created_at
		FROM operator_sessions
		WHERE token_hash = ? AND expires_at > ?
	`, tokenHash, now).Scan(
		&session.TokenHash,
		&session.CSRFHash,
		&session.OperatorID,
		&session.ExpiresAt,
		&session.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			_, _ = s.db.ExecContext(ctx, "DELETE FROM operator_sessions WHERE token_hash = ? AND expires_at <= ?", tokenHash, now)
			return OperatorSession{}, domain.ErrNotFound
		}
		return OperatorSession{}, fmt.Errorf("get operator session: %w", err)
	}
	return session, nil
}

func (s *Store) DeleteOperatorSession(ctx context.Context, tokenHash string) error {
	tokenHash = strings.ToLower(strings.TrimSpace(tokenHash))
	if !validSHA256Digest(tokenHash) {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM operator_sessions WHERE token_hash = ?", tokenHash); err != nil {
		return fmt.Errorf("delete operator session: %w", err)
	}
	return nil
}

func validSHA256Digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
