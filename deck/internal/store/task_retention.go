package store

import (
	"context"
	"fmt"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

// DeleteDraft removes only unexecuted backlog work. Referenced drafts must be
// archived instead so conversation promotion cannot create duplicate work.
func (s *Store) DeleteDraft(ctx context.Context, id string, version int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	task, err := getTaskTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if version < 1 || task.Version != version {
		return fmt.Errorf("task changed; refresh before deleting: %w", domain.ErrConflict)
	}
	var runs, references int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE task_id = ?", id).Scan(&runs); err != nil {
		return err
	}
	if task.Status != domain.TaskBacklog || runs != 0 || task.Archived || task.Workload != nil {
		return fmt.Errorf("only unarchived backlog drafts without runs or workload invocations can be deleted; archive this task instead: %w", domain.ErrConflict)
	}
	if err := tx.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM channel_messages WHERE promoted_task_id = ?) + (SELECT COUNT(*) FROM thread_tasks WHERE task_id = ?)", id, id).Scan(&references); err != nil {
		return err
	}
	var dependents int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks, json_each(tasks.depends_on_json) d WHERE d.value = ?", id).Scan(&dependents); err != nil {
		return err
	}
	if references != 0 || dependents != 0 {
		return fmt.Errorf("this draft is linked to a conversation; archive it instead: %w", domain.ErrConflict)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM events WHERE task_id = ?", id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM tasks WHERE id = ? AND version = ? AND NOT EXISTS (SELECT 1 FROM runs WHERE task_id = ?)", id, version, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return domain.ErrConflict
	}
	// The event has no foreign key to the removed draft, but retains its identity
	// for other open operator sessions to close stale drawers and refresh lists.
	event, err := insertEventTx(ctx, tx, nil, nil, nil, "task.deleted", "Draft task deleted", map[string]any{"task_id": id, "project_id": task.ProjectID})
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.publish(event)
	return nil
}

func (s *Store) SetTaskArchived(ctx context.Context, id string, version int, archived bool) (domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	task, err := getTaskTx(ctx, tx, id)
	if err != nil {
		return domain.Task{}, err
	}
	if version < 1 || task.Version != version {
		return domain.Task{}, fmt.Errorf("task changed; refresh before archiving or restoring: %w", domain.ErrConflict)
	}
	var active int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE task_id = ? AND status IN ('planning', 'waiting_decision', 'running', 'verifying')", id).Scan(&active); err != nil {
		return domain.Task{}, err
	}
	if active != 0 || (task.Status != domain.TaskBacklog && task.Status != domain.TaskDone && task.Status != domain.TaskBlocked && task.Status != domain.TaskFailed && task.Status != domain.TaskCancelled) {
		return domain.Task{}, fmt.Errorf("stop active work or return ready work to backlog before archiving: %w", domain.ErrConflict)
	}
	if task.Archived == archived {
		return task, nil
	}
	now := nowMillis()
	result, err := tx.ExecContext(ctx, "UPDATE tasks SET archived = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?", archived, now, id, version)
	if err != nil {
		return domain.Task{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return domain.Task{}, err
	}
	if count != 1 {
		return domain.Task{}, domain.ErrConflict
	}
	kind, message := "task.archived", "Task archived; execution history retained"
	if !archived {
		kind, message = "task.restored", "Task restored from archive"
	}
	event, err := insertEventTx(ctx, tx, &id, nil, nil, kind, message, map[string]any{"archived": archived})
	if err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	task.Archived, task.Version, task.UpdatedAt = archived, task.Version+1, now
	s.publish(event)
	return task, nil
}
