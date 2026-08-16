package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const staleProjectionThreshold = 2 * time.Minute

func (s *Store) RecordProjectionAccepted(ctx context.Context, projectID, producer string, occurredAt int64) error {
	projectID = strings.TrimSpace(projectID)
	producer = strings.TrimSpace(producer)
	if projectID == "" || producer == "" || occurredAt <= 0 {
		return fmt.Errorf("project, producer, and occurrence time are required: %w", domain.ErrInvalidInput)
	}
	now := nowMillis()
	stale := 0
	if now-occurredAt > staleProjectionThreshold.Milliseconds() {
		stale = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO projection_health (
			project_id, producer, connected_at, last_seen_at, last_success_at,
			last_occurred_at, accepted_count, stale_count, rejected_count,
			last_error, degraded_dependencies_json
		) VALUES (?, ?, ?, ?, ?, ?, 1, ?, 0, '', '[]')
		ON CONFLICT(project_id, producer) DO UPDATE SET
			last_seen_at = excluded.last_seen_at,
			last_success_at = excluded.last_success_at,
			last_occurred_at = excluded.last_occurred_at,
			accepted_count = projection_health.accepted_count + 1,
			stale_count = projection_health.stale_count + excluded.stale_count,
			last_error = '',
			degraded_dependencies_json = '[]'
	`, projectID, producer, now, now, now, occurredAt, stale)
	if err != nil {
		return fmt.Errorf("record accepted projection: %w", err)
	}
	return nil
}

func (s *Store) RecordProjectionRejected(
	ctx context.Context,
	projectID string,
	producer string,
	reason string,
	degradedDependencies []string,
) error {
	projectID = strings.TrimSpace(projectID)
	producer = strings.TrimSpace(producer)
	reason = strings.TrimSpace(reason)
	if projectID == "" || producer == "" || reason == "" {
		return fmt.Errorf("project, producer, and rejection reason are required: %w", domain.ErrInvalidInput)
	}
	dependencies := append([]string(nil), degradedDependencies...)
	sort.Strings(dependencies)
	now := nowMillis()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO projection_health (
			project_id, producer, connected_at, last_seen_at, accepted_count,
			stale_count, rejected_count, last_error, degraded_dependencies_json
		) VALUES (?, ?, ?, ?, 0, 0, 1, ?, ?)
		ON CONFLICT(project_id, producer) DO UPDATE SET
			last_seen_at = excluded.last_seen_at,
			rejected_count = projection_health.rejected_count + 1,
			last_error = excluded.last_error,
			degraded_dependencies_json = excluded.degraded_dependencies_json
	`, projectID, producer, now, now, reason, marshalJSON(dependencies))
	if err != nil {
		return fmt.Errorf("record rejected projection: %w", err)
	}
	return nil
}

func (s *Store) GetProviderStatus(ctx context.Context, projectID string) (domain.ProviderStatus, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.ProviderStatus{}, fmt.Errorf("project is required: %w", domain.ErrInvalidInput)
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE id = ?", projectID).Scan(&exists); err != nil {
		return domain.ProviderStatus{}, fmt.Errorf("inspect provider project: %w", err)
	}
	if exists != 1 {
		return domain.ProviderStatus{}, domain.ErrNotFound
	}
	now := nowMillis()
	status := domain.ProviderStatus{
		ProjectID: projectID, Connectivity: "unknown", SyncState: "unknown",
		LeaseState: "none", DegradedDependencies: []string{},
	}
	var lastSeen sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT MAX(a.last_seen_at)
		FROM agents a
		JOIN runs r ON r.agent_id = a.id
		JOIN tasks t ON t.id = r.task_id
		WHERE t.project_id = ?
	`, projectID).Scan(&lastSeen); err != nil {
		return domain.ProviderStatus{}, fmt.Errorf("load provider connectivity: %w", err)
	}
	if lastSeen.Valid {
		value := lastSeen.Int64
		status.LastSeenAt = &value
		age := now - value
		switch {
		case age <= (2 * time.Minute).Milliseconds():
			status.Connectivity = "connected"
		case age <= (5 * time.Minute).Milliseconds():
			status.Connectivity = "stale"
		default:
			status.Connectivity = "disconnected"
		}
	}

	var lastOccurred sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT MAX(last_occurred_at), COALESCE(SUM(stale_count), 0), COALESCE(SUM(rejected_count), 0)
		FROM projection_health WHERE project_id = ?
	`, projectID).Scan(&lastOccurred, &status.StaleProjectionCount, &status.RejectedProjectionCount); err != nil {
		return domain.ProviderStatus{}, fmt.Errorf("load projection health: %w", err)
	}
	var dependencies string
	err := s.db.QueryRowContext(ctx, `
		SELECT last_error, degraded_dependencies_json
		FROM projection_health WHERE project_id = ?
		ORDER BY last_seen_at DESC, producer LIMIT 1
	`, projectID).Scan(&status.LastError, &dependencies)
	if err != nil && err != sql.ErrNoRows {
		return domain.ProviderStatus{}, fmt.Errorf("load latest projection health: %w", err)
	}
	if err == nil {
		status.DegradedDependencies = unmarshalStrings(dependencies)
	}
	if lastOccurred.Valid {
		status.SyncLagMillis = now - lastOccurred.Int64
		switch {
		case status.LastError != "":
			status.SyncState = "rejected"
		case status.SyncLagMillis > (5 * time.Minute).Milliseconds():
			status.SyncState = "stale"
		case status.SyncLagMillis > time.Minute.Milliseconds():
			status.SyncState = "lagging"
		default:
			status.SyncState = "current"
		}
	} else if status.LastError != "" {
		status.SyncState = "rejected"
	}

	var leaseExpiresAt sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT MIN(r.lease_expires_at)
		FROM runs r JOIN tasks t ON t.id = r.task_id
		WHERE t.project_id = ?
		  AND r.status IN ('planning', 'waiting_decision', 'running', 'verifying')
		  AND r.lease_expires_at IS NOT NULL
	`, projectID).Scan(&leaseExpiresAt); err != nil {
		return domain.ProviderStatus{}, fmt.Errorf("load provider leases: %w", err)
	}
	if leaseExpiresAt.Valid {
		value := leaseExpiresAt.Int64
		status.LeaseExpiresAt = &value
		switch {
		case value <= now:
			status.LeaseState = "expired"
		case value-now <= (30 * time.Second).Milliseconds():
			status.LeaseState = "expiring"
		default:
			status.LeaseState = "active"
		}
	}
	return status, nil
}
