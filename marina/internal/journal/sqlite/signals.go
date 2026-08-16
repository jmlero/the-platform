package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/signals"
)

func (store *Store) RecordValidatedSignal(
	ctx context.Context,
	raw signals.RawSignal,
	validated signals.ValidatedSignal,
) (signals.ValidatedSignal, bool, error) {
	if strings.TrimSpace(raw.ID) == "" || strings.TrimSpace(raw.Source) == "" || strings.TrimSpace(raw.ExternalID) == "" ||
		strings.TrimSpace(validated.ID) == "" || validated.RawSignalID != raw.ID || validated.ProjectID != raw.ProjectID ||
		validated.Source != raw.Source || validated.Type != raw.Type || validated.DedupeKey == "" ||
		validated.CorrelationID == "" || validated.Trust != signals.TrustValidatedObservation {
		return signals.ValidatedSignal{}, false, execution.ErrInvalidInput
	}
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		return signals.ValidatedSignal{}, false, fmt.Errorf("encode raw signal: %w", err)
	}
	validatedJSON, err := json.Marshal(validated)
	if err != nil {
		return signals.ValidatedSignal{}, false, fmt.Errorf("encode validated signal: %w", err)
	}
	if stored, found, err := store.validatedSignalByExternalID(ctx, raw.Source, raw.ExternalID); err != nil {
		return signals.ValidatedSignal{}, false, err
	} else if found {
		if stored.DedupeKey != validated.DedupeKey {
			return signals.ValidatedSignal{}, false, execution.ErrConflict
		}
		return stored, false, nil
	}
	result, err := store.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO validated_signals (
			id, raw_signal_id, project_id, source, external_id, signal_type,
			classification, severity, correlation_id, dedupe_key, raw_json,
			validated_json, observed_at, validated_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, validated.ID, raw.ID, raw.ProjectID, raw.Source, raw.ExternalID, raw.Type,
		validated.Classification, validated.Severity, validated.CorrelationID, validated.DedupeKey,
		string(rawJSON), string(validatedJSON), validated.ObservedAt.UnixMilli(), validated.ValidatedAt.UnixMilli(), store.now().UnixMilli())
	if err != nil {
		return signals.ValidatedSignal{}, false, fmt.Errorf("record validated signal: %w", err)
	}
	created, _ := result.RowsAffected()
	var storedJSON string
	if err := store.db.QueryRowContext(ctx,
		"SELECT validated_json FROM validated_signals WHERE dedupe_key = ?", validated.DedupeKey,
	).Scan(&storedJSON); err != nil {
		if stored, found, lookupErr := store.validatedSignalByExternalID(ctx, raw.Source, raw.ExternalID); lookupErr == nil && found {
			if stored.DedupeKey != validated.DedupeKey {
				return signals.ValidatedSignal{}, false, execution.ErrConflict
			}
			return stored, false, nil
		}
		return signals.ValidatedSignal{}, false, fmt.Errorf("load validated signal: %w", err)
	}
	var stored signals.ValidatedSignal
	if err := json.Unmarshal([]byte(storedJSON), &stored); err != nil {
		return signals.ValidatedSignal{}, false, fmt.Errorf("decode validated signal: %w", err)
	}
	return stored, created == 1, nil
}

func (store *Store) validatedSignalByExternalID(
	ctx context.Context,
	source string,
	externalID string,
) (signals.ValidatedSignal, bool, error) {
	var encoded string
	err := store.db.QueryRowContext(ctx,
		"SELECT validated_json FROM validated_signals WHERE source = ? AND external_id = ?", source, externalID,
	).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return signals.ValidatedSignal{}, false, nil
	}
	if err != nil {
		return signals.ValidatedSignal{}, false, fmt.Errorf("inspect external signal identity: %w", err)
	}
	var stored signals.ValidatedSignal
	if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
		return signals.ValidatedSignal{}, false, fmt.Errorf("decode external signal identity: %w", err)
	}
	return stored, true, nil
}

func (store *Store) GetValidatedSignal(
	ctx context.Context,
	id string,
) (signals.RawSignal, signals.ValidatedSignal, error) {
	var rawJSON string
	var validatedJSON string
	if err := store.db.QueryRowContext(ctx,
		"SELECT raw_json, validated_json FROM validated_signals WHERE id = ?", strings.TrimSpace(id),
	).Scan(&rawJSON, &validatedJSON); err != nil {
		return signals.RawSignal{}, signals.ValidatedSignal{}, wrapNotFound(err)
	}
	var raw signals.RawSignal
	var validated signals.ValidatedSignal
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		return signals.RawSignal{}, signals.ValidatedSignal{}, fmt.Errorf("decode raw signal: %w", err)
	}
	if err := json.Unmarshal([]byte(validatedJSON), &validated); err != nil {
		return signals.RawSignal{}, signals.ValidatedSignal{}, fmt.Errorf("decode validated signal: %w", err)
	}
	return raw, validated, nil
}
