package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func (s *Store) listCommandsForTask(ctx context.Context, taskID string) ([]domain.Command, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.run_id, c.sequence, c.type, c.status, c.reason,
			c.requested_by, c.result, c.run_version, c.fence_token, c.created_at, c.updated_at
		FROM commands c JOIN runs r ON r.id = c.run_id
		WHERE r.task_id = ? ORDER BY c.created_at DESC, c.id
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task commands: %w", err)
	}
	defer rows.Close()
	commands := make([]domain.Command, 0)
	for rows.Next() {
		command, scanErr := scanCommand(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan task command: %w", scanErr)
		}
		commands = append(commands, command)
	}
	return commands, rows.Err()
}

func taskDisplayState(
	task domain.Task,
	commands []domain.Command,
	evidence []domain.EvidenceRecord,
	provider domain.ProviderStatus,
) string {
	if task.Status == domain.TaskVerifying {
		for _, item := range evidence {
			if item.AdmissionStatus == "stale" || item.AdmissionStatus == "rejected" {
				return "stale-evidence"
			}
		}
	}
	for _, command := range commands {
		if command.Type == domain.CommandRetry && (command.Status == "pending" || command.Status == "acknowledged") {
			return "retrying"
		}
	}
	if task.Status == domain.TaskRunning && (provider.SyncState == "lagging" || provider.SyncState == "stale" || provider.SyncState == "rejected") {
		return "syncing"
	}
	switch task.Status {
	case domain.TaskWaitingDecision:
		return "waiting"
	case domain.TaskDone:
		return "completed"
	case domain.TaskFailed, domain.TaskCancelled:
		return "blocked"
	default:
		return strings.ReplaceAll(task.Status, "_", "-")
	}
}

func buildTaskTrail(
	events []domain.Event,
	plans []domain.PlanRevision,
	decisions []domain.Decision,
	commands []domain.Command,
	evidence []domain.EvidenceRecord,
	operations []domain.OperationalState,
) []domain.TrailEntry {
	trail := make([]domain.TrailEntry, 0, len(events)+len(plans)+len(decisions)+len(commands)+len(evidence)+len(operations)*2)
	for _, event := range events {
		trail = append(trail, domain.TrailEntry{
			ID: "event:" + strconv.FormatInt(event.ID, 10), Stage: stageForEvent(event.Type),
			Type: event.Type, Title: event.Message, ResourceID: strconv.FormatInt(event.ID, 10),
			Payload: event.Payload, OccurredAt: event.CreatedAt,
		})
	}
	for _, plan := range plans {
		trail = append(trail, domain.TrailEntry{
			ID: "plan:" + plan.ID, Stage: "change", Type: "plan.revision",
			Title: fmt.Sprintf("Plan revision %d", plan.Revision), ResourceID: plan.ID,
			Payload:    map[string]any{"risk_class": plan.RiskClass, "source_revision": plan.SourceRevision},
			OccurredAt: plan.CreatedAt,
		})
	}
	for _, decision := range decisions {
		trail = append(trail, domain.TrailEntry{
			ID: "decision:" + decision.ID, Stage: "decision", Type: "decision." + decision.Status,
			Title: decision.Question, ResourceID: decision.ID,
			Payload:    map[string]any{"answer": decision.Answer, "risk_class": decision.RiskClass},
			OccurredAt: decision.CreatedAt,
		})
	}
	for _, command := range commands {
		trail = append(trail, domain.TrailEntry{
			ID: "command:" + command.ID, Stage: "change", Type: "command." + command.Type,
			Title: command.Reason, ResourceID: command.ID,
			Payload:    map[string]any{"status": command.Status, "requested_by": command.RequestedBy},
			OccurredAt: command.CreatedAt,
		})
	}
	for _, item := range evidence {
		trail = append(trail, domain.TrailEntry{
			ID: "evidence:" + item.ID, Stage: "verification", Type: "evidence." + item.Type,
			Title: item.Command, ResourceID: item.ID,
			Payload:    map[string]any{"outcome": item.Outcome, "admission_status": item.AdmissionStatus, "verifier": item.Verifier},
			OccurredAt: item.ObservedAt,
		})
	}
	for _, operation := range operations {
		trail = append(trail, domain.TrailEntry{
			ID: "operation:" + operation.ID, Stage: "delivery", Type: "rollout." + operation.RolloutState,
			Title: operation.Environment + " rollout: " + operation.RolloutState, ResourceID: operation.ID,
			Payload:    map[string]any{"rollback_state": operation.RollbackState, "policy_version": operation.PolicyVersion},
			OccurredAt: operation.UpdatedAt,
		})
		if operation.ObservationStartedAt != nil {
			trail = append(trail, domain.TrailEntry{
				ID: "observation:" + operation.ID, Stage: "observation", Type: "observation.window",
				Title: operation.Environment + " observation window", ResourceID: operation.ID,
				Payload:    map[string]any{"ends_at": operation.ObservationEndsAt, "outcome": operation.Outcome},
				OccurredAt: *operation.ObservationStartedAt,
			})
		}
	}
	sort.SliceStable(trail, func(i, j int) bool {
		if trail[i].OccurredAt == trail[j].OccurredAt {
			return trail[i].ID < trail[j].ID
		}
		return trail[i].OccurredAt < trail[j].OccurredAt
	})
	return trail
}

func stageForEvent(eventType string) string {
	switch {
	case strings.Contains(eventType, "signal"):
		return "signal"
	case strings.HasPrefix(eventType, "decision."):
		return "decision"
	case strings.HasPrefix(eventType, "evidence."), strings.Contains(eventType, "verif"):
		return "verification"
	case strings.HasPrefix(eventType, "operation."), strings.Contains(eventType, "rollout"), strings.Contains(eventType, "deploy"):
		return "delivery"
	case strings.Contains(eventType, "observ"):
		return "observation"
	case strings.Contains(eventType, "completed"), strings.Contains(eventType, "failed"), strings.Contains(eventType, "blocked"), strings.Contains(eventType, "cancelled"):
		return "outcome"
	default:
		return "change"
	}
}
