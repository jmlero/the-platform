package store

import (
	"context"
	"fmt"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

// Read accessors that only tests need.

func (s *Store) TaskDependenciesSatisfied(ctx context.Context, id string) (bool, error) {
	var eligible bool
	err := s.db.QueryRowContext(ctx, "SELECT "+dependenciesEligibleSQL+" FROM tasks WHERE id = ?", id).Scan(&eligible)
	return eligible, err
}

func (s *Store) ListPlanRevisions(ctx context.Context, runID string) ([]domain.PlanRevision, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+planRevisionColumns+" FROM plan_revisions WHERE run_id = ? ORDER BY revision", runID,
	)
	if err != nil {
		return nil, fmt.Errorf("list plan revisions: %w", err)
	}
	defer rows.Close()
	revisions := make([]domain.PlanRevision, 0)
	for rows.Next() {
		revision, scanErr := scanPlanRevision(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan plan revision: %w", scanErr)
		}
		revisions = append(revisions, revision)
	}
	return revisions, rows.Err()
}

func (s *Store) GetChannelMessage(ctx context.Context, id string) (domain.ChannelMessage, error) {
	message, err := scanChannelMessage(s.db.QueryRowContext(ctx, "SELECT "+channelMessageColumns+" FROM channel_messages WHERE id=?", id))
	if err != nil {
		return domain.ChannelMessage{}, normalizeStoreError(err)
	}
	return message, nil
}
