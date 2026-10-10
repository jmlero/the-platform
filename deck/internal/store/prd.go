package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

// ApprovePRD is an operator-only handoff. The immutable message owns the draft;
// Git becomes authoritative only when the normal repository task lands.
func (s *Store) ApprovePRD(ctx context.Context, messageID string, mutation Mutation, in domain.PRDApproval) (domain.Task, error) {
	if !strings.HasPrefix(mutation.ActorID, "operator:") || in.Budget.TimeMinutes < 1 || in.Budget.MaxTokens < 1 || in.Budget.MaxAttempts < 2 || in.Budget.MaxConcurrency != 1 {
		return domain.Task{}, domain.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE channel_messages SET id=id WHERE id=?", messageID); err != nil {
		return domain.Task{}, err
	}
	msg, err := scanChannelMessage(tx.QueryRowContext(ctx, "SELECT "+channelMessageColumns+" FROM channel_messages WHERE id=?", messageID))
	if err != nil {
		return domain.Task{}, normalizeStoreError(err)
	}
	if msg.PRD == nil || msg.AuthorType != "agent" || msg.ThreadID == "" || msg.PRD.Validate() != nil || msg.PRD.Digest != msg.PRD.ContentDigest() || msg.PRD.MessageID != msg.ID || in.Digest != msg.PRD.Digest {
		return domain.Task{}, fmt.Errorf("PRD draft changed or is unavailable: %w", domain.ErrConflict)
	}
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	approvalDigest := hex.EncodeToString(sum[:])
	receipt, replay, exists, err := beginIdempotentMutation(ctx, tx, mutation, msg.ProjectID, "prd.approve", messageID, in)
	if err != nil {
		return domain.Task{}, err
	}
	if exists {
		var task domain.Task
		err = decodeIdempotencyResult(replay, &task)
		return task, err
	}
	if msg.PromotedTaskID != nil {
		task, err := scanTask(tx.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id=?", *msg.PromotedTaskID))
		if err != nil {
			return task, err
		}
		if task.PRD == nil || task.PRD.ApprovalDigest != approvalDigest {
			return domain.Task{}, fmt.Errorf("PRD already approved with different input: %w", domain.ErrConflict)
		}
		if err = finishIdempotentMutation(ctx, tx, receipt, task); err != nil {
			return domain.Task{}, err
		}
		return task, tx.Commit()
	}
	draft := *msg.PRD
	draft.ApprovalDigest = approvalDigest
	draft.ApprovedBy = mutation.ActorID
	draft.ApprovedAt = nowMillis()
	input, err := validateTaskInput(domain.CreateTaskInput{ProjectID: msg.ProjectID, Title: "Publish PRD: " + draft.Title, Description: "Publish the approved PRD verbatim at " + draft.Path + ". Change only this file; do not implement the proposed product.\n\n" + draft.Content, AcceptanceCriteria: "The exact approved PRD is committed at " + draft.Path + " and lands on the repository base branch after independent review, configured clean checks and required CI. No unrelated files change.", Status: domain.TaskReady, Budget: &in.Budget, ContextMessageIDs: []string{msg.ID}})
	if err != nil {
		return domain.Task{}, err
	}
	profile, err := admittedProfile(ctx, tx, msg.ProjectID, domain.WorkloadInvocation{ProfileID: "repository/v2", ProfileDigest: in.ProfileDigest, Task: input}, *input.Budget)
	if err != nil {
		return domain.Task{}, err
	}
	selected, err := selectTaskContext(ctx, tx, msg.ProjectID, input.ContextMessageIDs, true)
	if err != nil {
		return domain.Task{}, err
	}
	if selected == nil || selected.Repository == nil || !reflect.DeepEqual(*selected.Repository, draft.Repository) {
		return domain.Task{}, fmt.Errorf("repository binding changed; request a fresh PRD draft: %w", domain.ErrConflict)
	}
	task := domain.Task{ID: newID("tsk"), ProjectID: msg.ProjectID, Title: input.Title, Description: input.Description, AcceptanceCriteria: input.AcceptanceCriteria, Status: domain.TaskReady, Priority: input.Priority, Budget: *input.Budget, BudgetMinutes: input.BudgetMinutes, Version: 1, CreatedAt: nowMillis(), UpdatedAt: nowMillis(), DisplayState: "ready", Context: selected, Workload: profile, PRD: &draft}
	_, err = tx.ExecContext(ctx, `INSERT INTO tasks(id,project_id,title,description,acceptance_criteria,status,priority,budget_minutes,deadline_at,max_attempts,max_tokens,max_cost_minor,currency,max_concurrency,authority_json,version,created_at,updated_at,context_json,workload_json,prd_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, task.ID, task.ProjectID, task.Title, task.Description, task.AcceptanceCriteria, task.Status, task.Priority, task.BudgetMinutes, nullableInt64(task.Budget.DeadlineAt), task.Budget.MaxAttempts, task.Budget.MaxTokens, task.Budget.MaxCostMinor, task.Budget.Currency, task.Budget.MaxConcurrency, marshalJSON(task.Budget.Authority), task.Version, task.CreatedAt, task.UpdatedAt, marshalJSON(selected), marshalJSON(profile), marshalJSON(task.PRD))
	if err != nil {
		return domain.Task{}, err
	}
	if _, err = linkThreadTaskTx(ctx, tx, msg.ThreadID, task.ID); err != nil {
		return domain.Task{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE channel_messages SET promoted_task_id=? WHERE id=?", task.ID, msg.ID); err != nil {
		return domain.Task{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_messages(id,channel_id,project_id,author_type,author_id,body,references_json,created_at,thread_id) VALUES(?,?,?,?,?,?,?,?,?)`, newID("msg"), msg.ChannelID, msg.ProjectID, "human", mutation.ActorID, "Approved PRD: "+draft.Title+" ("+draft.Path+"). Publication task authorized.", marshalJSON(domain.CanonicalReferences{TaskIDs: []string{task.ID}}), nowMillis(), msg.ThreadID)
	if err != nil {
		return domain.Task{}, err
	}
	event, err := insertEventTx(ctx, tx, &task.ID, nil, nil, "prd.approved", "PRD approved for repository publication", map[string]any{"project_id": task.ProjectID, "thread_id": msg.ThreadID, "message_id": msg.ID, "digest": draft.Digest, "actor_id": mutation.ActorID})
	if err != nil {
		return domain.Task{}, err
	}
	if err = finishIdempotentMutation(ctx, tx, receipt, task); err != nil {
		return domain.Task{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	s.publish(event)
	return task, nil
}
