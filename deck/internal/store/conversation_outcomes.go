package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func outcomeDigest(in domain.ConversationOutcomeInput) string {
	encoded, _ := json.Marshal(in)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// PublishConversationOutcome is an atomic, replay-safe handoff from one
// conversation turn into the canonical thread and, optionally, one new task.
func (s *Store) PublishConversationOutcome(ctx context.Context, in domain.ConversationOutcomeInput) (domain.ConversationOutcome, error) {
	in.InvocationMessageID = strings.TrimSpace(in.InvocationMessageID)
	in.AgentID = strings.TrimSpace(in.AgentID)
	in.Body = strings.TrimSpace(in.Body)
	in.ResultDigest = strings.TrimSpace(in.ResultDigest)
	in.FollowupTaskID = strings.TrimSpace(in.FollowupTaskID)
	in.ProfileID = strings.TrimSpace(in.ProfileID)
	in.ProfileDigest = strings.TrimSpace(in.ProfileDigest)
	if in.InvocationMessageID == "" || in.AgentID == "" || in.Body == "" || len(in.Body) > 8<<10 ||
		in.ResultDigest == "" || len(in.ResultDigest) > 128 || (in.Status != "completed" && in.Status != "blocked") ||
		(in.Status == "blocked" && in.Task != nil) || (in.Task == nil && in.FollowupTaskID != "") {
		return domain.ConversationOutcome{}, fmt.Errorf("invalid conversation outcome: %w", domain.ErrInvalidInput)
	}
	if in.Task != nil && (in.Task.Status != "" && in.Task.Status != domain.TaskReady && in.Task.Status != domain.TaskBacklog) {
		return domain.ConversationOutcome{}, fmt.Errorf("delegated task must be Backlog or Ready: %w", domain.ErrInvalidInput)
	}
	if (in.Task == nil || in.Task.Status != domain.TaskReady) && (in.ProfileID != "" || in.ProfileDigest != "") {
		return domain.ConversationOutcome{}, fmt.Errorf("only Ready delegation can bind an installed profile: %w", domain.ErrInvalidInput)
	}
	digest := outcomeDigest(in)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ConversationOutcome{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE channel_messages SET id=id WHERE id=?", in.InvocationMessageID); err != nil {
		return domain.ConversationOutcome{}, err
	}
	invocation, err := scanChannelMessage(tx.QueryRowContext(ctx, "SELECT "+channelMessageColumns+" FROM channel_messages WHERE id=?", in.InvocationMessageID))
	if err != nil {
		return domain.ConversationOutcome{}, err
	}
	if invocation.AuthorType != "human" || invocation.InvokedAgentID != in.AgentID || invocation.InvocationRequestID == "" || invocation.ThreadID == "" {
		return domain.ConversationOutcome{}, fmt.Errorf("outcome is not bound to the invoked agent: %w", domain.ErrConflict)
	}
	var prior domain.ConversationOutcome
	var priorDigest string
	err = tx.QueryRowContext(ctx, `SELECT message_id,COALESCE(task_id,''),COALESCE(followup_task_id,''),status,result_digest,request_digest FROM conversation_outcomes WHERE invocation_message_id=?`, in.InvocationMessageID).
		Scan(&prior.MessageID, &prior.TaskID, &prior.FollowupTaskID, &prior.Status, &prior.ResultDigest, &priorDigest)
	if err == nil {
		if priorDigest != digest {
			return domain.ConversationOutcome{}, fmt.Errorf("conversation outcome changed on replay: %w", domain.ErrConflict)
		}
		prior.InvocationMessageID = in.InvocationMessageID
		return prior, nil
	}
	if err != sql.ErrNoRows {
		return domain.ConversationOutcome{}, err
	}
	if in.Task != nil && invocation.PromotedTaskID != nil {
		return domain.ConversationOutcome{}, fmt.Errorf("invocation already has a task: %w", domain.ErrConflict)
	}
	out := domain.ConversationOutcome{InvocationMessageID: in.InvocationMessageID, MessageID: newID("msg"), FollowupTaskID: in.FollowupTaskID, Status: in.Status, ResultDigest: in.ResultDigest}
	refs := domain.CanonicalReferences{}
	if in.Task != nil {
		input := *in.Task
		if input.ProjectID != "" && input.ProjectID != invocation.ProjectID {
			return out, fmt.Errorf("delegated task escaped project: %w", domain.ErrInvalidInput)
		}
		input.ProjectID = invocation.ProjectID
		if input.Description == "" {
			input.Description = invocation.Body
		}
		if input.Status == domain.TaskReady && (strings.TrimSpace(input.AcceptanceCriteria) == "" || input.Budget == nil) {
			return out, fmt.Errorf("ready delegation needs criteria and explicit budget: %w", domain.ErrInvalidInput)
		}
		input.ContextMessageIDs = append(input.ContextMessageIDs, invocation.ID)
		validated, err := validateTaskInput(input)
		if err != nil {
			return out, err
		}
		var profile *domain.WorkloadProfile
		if validated.Status == domain.TaskReady {
			if in.ProfileID != "repository/v1" || in.ProfileDigest == "" || validated.Budget.MaxAttempts != 2 {
				return out, fmt.Errorf("Ready delegation requires the installed repository profile and two attempts: %w", domain.ErrInvalidInput)
			}
			profile, err = admittedProfile(ctx, tx, invocation.ProjectID, domain.WorkloadInvocation{ProfileID: in.ProfileID, ProfileDigest: in.ProfileDigest, Task: validated}, *validated.Budget)
			if err != nil {
				return out, err
			}
		}
		if in.FollowupTaskID != "" {
			var projectID, threadID, status string
			err = tx.QueryRowContext(ctx, `SELECT t.project_id,tt.thread_id,t.status FROM tasks t JOIN thread_tasks tt ON tt.task_id=t.id WHERE t.id=?`, in.FollowupTaskID).Scan(&projectID, &threadID, &status)
			if err != nil || projectID != invocation.ProjectID || threadID != invocation.ThreadID || status != domain.TaskDone {
				return out, fmt.Errorf("follow-up requires a completed task from this thread: %w", domain.ErrInvalidInput)
			}
			refs.TaskIDs = append(refs.TaskIDs, in.FollowupTaskID)
		}
		selected, err := selectTaskContext(ctx, tx, validated.ProjectID, validated.ContextMessageIDs, validated.Status == domain.TaskReady)
		if err != nil {
			return out, err
		}
		now := nowMillis()
		out.TaskID = newID("tsk")
		_, err = tx.ExecContext(ctx, `INSERT INTO tasks (id,project_id,title,description,acceptance_criteria,status,priority,budget_minutes,deadline_at,max_attempts,max_tokens,max_cost_minor,currency,max_concurrency,authority_json,version,created_at,updated_at,context_json,workload_json)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, out.TaskID, validated.ProjectID, validated.Title, validated.Description, validated.AcceptanceCriteria, validated.Status, validated.Priority, validated.BudgetMinutes, nullableInt64(validated.Budget.DeadlineAt), validated.Budget.MaxAttempts, validated.Budget.MaxTokens, validated.Budget.MaxCostMinor, validated.Budget.Currency, validated.Budget.MaxConcurrency, marshalJSON(validated.Budget.Authority), 1, now, now, marshalJSON(selected), marshalJSON(profile))
		if err != nil {
			return out, err
		}
		if _, err = linkThreadTaskTx(ctx, tx, invocation.ThreadID, out.TaskID); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE channel_messages SET promoted_task_id=? WHERE id=? AND promoted_task_id IS NULL`, out.TaskID, invocation.ID); err != nil {
			return out, err
		}
		refs.TaskIDs = append(refs.TaskIDs, out.TaskID)
	}
	createdAt := nowMillis()
	if _, err := tx.ExecContext(ctx, `INSERT INTO channel_messages (id,channel_id,project_id,author_type,author_id,body,references_json,created_at,thread_id) VALUES (?,?,?,?,?,?,?,?,?)`, out.MessageID, invocation.ChannelID, invocation.ProjectID, "agent", in.AgentID, in.Body, marshalJSON(refs), createdAt, invocation.ThreadID); err != nil {
		return out, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_outcomes(invocation_message_id,message_id,task_id,followup_task_id,status,result_digest,request_digest,created_at) VALUES (?,?,?,?,?,?,?,?)`, invocation.ID, out.MessageID, nullableText(out.TaskID), nullableText(out.FollowupTaskID), out.Status, out.ResultDigest, digest, createdAt); err != nil {
		return out, err
	}
	event, err := insertEventTx(ctx, tx, nullableStringValue(out.TaskID), nil, &in.AgentID, "conversation.outcome.published", "Conversation outcome published", map[string]any{"project_id": invocation.ProjectID, "thread_id": invocation.ThreadID, "message_id": out.MessageID, "invocation_message_id": invocation.ID, "task_id": out.TaskID, "followup_task_id": out.FollowupTaskID})
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	s.publish(event)
	return out, nil
}

func nullableStringValue(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (s *Store) exportConversationOutcomes(ctx context.Context, projectID string) ([]portableConversationOutcome, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT o.invocation_message_id,o.message_id,COALESCE(o.task_id,''),COALESCE(o.followup_task_id,''),o.status,o.result_digest,o.request_digest,o.created_at
		FROM conversation_outcomes o JOIN channel_messages m ON m.id=o.invocation_message_id WHERE m.project_id=? ORDER BY o.created_at,o.invocation_message_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []portableConversationOutcome{}
	for rows.Next() {
		var item portableConversationOutcome
		if err := rows.Scan(&item.InvocationMessageID, &item.MessageID, &item.TaskID, &item.FollowupTaskID, &item.Status, &item.ResultDigest, &item.RequestDigest, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
