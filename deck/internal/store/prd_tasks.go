package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func jsonDigest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func prdPublicationTx(ctx context.Context, tx *sql.Tx, id string) (domain.Task, error) {
	task, err := getTaskTx(ctx, tx, id)
	if err != nil {
		return task, err
	}
	if task.PRD == nil || task.PRD.Digest != task.PRD.ContentDigest() || task.PRD.ApprovalDigest == "" || task.Status != domain.TaskDone || task.Landing == nil || task.Archived {
		return task, fmt.Errorf("PRD must first land with evidence: %w", domain.ErrConflict)
	}
	if err = validatePRDLandingTx(ctx, tx, task); err != nil {
		return task, err
	}
	return task, nil
}
func validatePRDLandingTx(ctx context.Context, tx *sql.Tx, task domain.Task) error {
	return validatePRDLandingAtTx(ctx, tx, task, nowMillis())
}
func validatePRDLandingAtTx(ctx context.Context, tx *sql.Tx, task domain.Task, at int64) error {
	if task.Status != domain.TaskDone || task.Landing == nil || task.Archived {
		return domain.ErrConflict
	}
	var criteria string
	var finished int64
	if err := tx.QueryRowContext(ctx, "SELECT acceptance_criteria_digest,finished_at FROM runs WHERE id=? AND task_id=? AND status='completed'", task.Landing.RunID, task.ID).Scan(&criteria, &finished); err != nil {
		return domain.ErrConflict
	}
	if finished > at || task.Landing.ObservedAt > at {
		return domain.ErrConflict
	}
	sum := sha256.Sum256([]byte(task.AcceptanceCriteria))
	if criteria != hex.EncodeToString(sum[:]) {
		return domain.ErrConflict
	}
	for _, id := range task.Landing.EvidenceIDs {
		var n int
		err := tx.QueryRowContext(ctx, "SELECT count(*) FROM evidence_records WHERE id=? AND run_id=? AND source_revision=? AND outcome='passed' AND admission_status='current' AND (expires_at IS NULL OR expires_at>?)", id, task.Landing.RunID, task.Landing.Commit, at).Scan(&n)
		if err != nil {
			return err
		}
		if n != 1 {
			return domain.ErrConflict
		}
	}
	return nil
}
func buildPRDTaskPlan(ctx context.Context, tx *sql.Tx, inv domain.ChannelMessage, messageID string, in domain.PRDTaskProposal) (*domain.PRDTaskPlan, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	publication, err := prdPublicationTx(ctx, tx, in.PublicationTaskID)
	if err != nil {
		return nil, err
	}
	var thread string
	if err = tx.QueryRowContext(ctx, "SELECT thread_id FROM thread_tasks WHERE task_id=?", publication.ID).Scan(&thread); err != nil || thread != inv.ThreadID || publication.ProjectID != inv.ProjectID || publication.Landing.Commit != in.Revision {
		return nil, fmt.Errorf("proposal needs this thread's exact landed PRD: %w", domain.ErrConflict)
	}
	plan := &domain.PRDTaskPlan{PRDTaskProposal: in, Source: domain.PRDReference{PublicationTaskID: publication.ID, PlanMessageID: messageID, Path: publication.PRD.Path, Revision: in.Revision, Digest: publication.PRD.Digest, Repository: publication.PRD.Repository}}
	plan.Digest = plan.ContentDigest()
	return plan, nil
}

func (s *Store) ApprovePRDTasks(ctx context.Context, messageID string, mutation Mutation, in domain.PRDApproval) ([]domain.Task, error) {
	if !strings.HasPrefix(mutation.ActorID, "operator:") || in.Budget.TimeMinutes < 1 || in.Budget.MaxTokens < 1 || in.Budget.MaxAttempts < 2 || in.Budget.MaxConcurrency != 1 {
		return nil, domain.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE channel_messages SET id=id WHERE id=?", messageID); err != nil {
		return nil, err
	}
	msg, err := scanChannelMessage(tx.QueryRowContext(ctx, "SELECT "+channelMessageColumns+" FROM channel_messages WHERE id=?", messageID))
	if err != nil {
		return nil, normalizeStoreError(err)
	}
	p := msg.PRDTasks
	if p == nil || p.Validate() != nil || p.Digest != p.ContentDigest() || p.Digest != in.Digest || p.Source.PlanMessageID != messageID || msg.AuthorType != "agent" || msg.ThreadID == "" {
		return nil, domain.ErrConflict
	}
	receipt, replay, exists, err := beginIdempotentMutation(ctx, tx, mutation, msg.ProjectID, "prd.tasks.approve", messageID, in)
	if err != nil {
		return nil, err
	}
	if exists {
		var tasks []domain.Task
		err = decodeIdempotencyResult(replay, &tasks)
		return tasks, err
	}
	digest := jsonDigest(in)
	if p.ApprovalDigest != "" {
		if p.ApprovalDigest != digest {
			return nil, domain.ErrConflict
		}
		tasks := []domain.Task{}
		for _, id := range p.TaskIDs {
			task, err := getTaskTx(ctx, tx, id)
			if err != nil {
				return nil, err
			}
			tasks = append(tasks, task)
		}
		if err = finishIdempotentMutation(ctx, tx, receipt, tasks); err != nil {
			return nil, err
		}
		return tasks, tx.Commit()
	}
	publication, err := prdPublicationTx(ctx, tx, p.PublicationTaskID)
	if err != nil {
		return nil, err
	}
	if publication.Landing.Commit != p.Revision || publication.PRD.Path != p.Source.Path || publication.PRD.Digest != p.Source.Digest || publication.PRD.Repository != p.Source.Repository {
		return nil, domain.ErrConflict
	}
	selected, err := selectTaskContext(ctx, tx, msg.ProjectID, []string{publication.PRD.MessageID, msg.ID}, true)
	if err != nil {
		return nil, err
	}
	if selected == nil || selected.Repository == nil || !reflect.DeepEqual(*selected.Repository, p.Source.Repository) {
		return nil, fmt.Errorf("PRD repository changed: %w", domain.ErrConflict)
	}
	tasks := []domain.Task{}
	previous := publication.ID
	for _, draft := range p.Tasks {
		input, err := validateTaskInput(domain.CreateTaskInput{ProjectID: msg.ProjectID, Title: draft.Title, Description: draft.Description + "\n\nPRD: " + p.Source.Path + " at " + p.Revision, AcceptanceCriteria: draft.AcceptanceCriteria, Status: domain.TaskReady, Budget: &in.Budget})
		if err != nil {
			return nil, err
		}
		profile, err := admittedProfile(ctx, tx, msg.ProjectID, domain.WorkloadInvocation{ProfileID: "repository/v2", ProfileDigest: in.ProfileDigest, Task: input}, *input.Budget)
		if err != nil {
			return nil, err
		}
		now := nowMillis()
		source := p.Source
		task := domain.Task{ID: newID("tsk"), ProjectID: msg.ProjectID, Title: input.Title, Description: input.Description, AcceptanceCriteria: input.AcceptanceCriteria, Status: domain.TaskReady, Priority: input.Priority, Budget: *input.Budget, BudgetMinutes: input.BudgetMinutes, Version: 1, CreatedAt: now, UpdatedAt: now, DisplayState: "ready", Context: selected, Workload: profile, DependsOn: []string{previous}, PRDReference: &source}
		_, err = tx.ExecContext(ctx, `INSERT INTO tasks(id,project_id,title,description,acceptance_criteria,status,priority,budget_minutes,deadline_at,max_attempts,max_tokens,max_cost_minor,currency,max_concurrency,authority_json,version,created_at,updated_at,context_json,workload_json,depends_on_json,prd_reference_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, task.ID, task.ProjectID, task.Title, task.Description, task.AcceptanceCriteria, task.Status, task.Priority, task.BudgetMinutes, nullableInt64(task.Budget.DeadlineAt), task.Budget.MaxAttempts, task.Budget.MaxTokens, task.Budget.MaxCostMinor, task.Budget.Currency, task.Budget.MaxConcurrency, marshalJSON(task.Budget.Authority), task.Version, now, now, marshalJSON(selected), marshalJSON(profile), marshalJSON(task.DependsOn), marshalJSON(task.PRDReference))
		if err != nil {
			return nil, err
		}
		if _, err = linkThreadTaskTx(ctx, tx, msg.ThreadID, task.ID); err != nil {
			return nil, err
		}
		p.TaskIDs = append(p.TaskIDs, task.ID)
		tasks = append(tasks, task)
		previous = task.ID
	}
	p.ApprovalDigest = digest
	p.ApprovedBy = mutation.ActorID
	p.ApprovedAt = nowMillis()
	if _, err = tx.ExecContext(ctx, "UPDATE channel_messages SET prd_tasks_json=?,references_json=? WHERE id=?", marshalJSON(p), marshalJSON(domain.CanonicalReferences{TaskIDs: p.TaskIDs}), msg.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO channel_messages(id,channel_id,project_id,author_type,author_id,body,references_json,created_at,thread_id) VALUES(?,?,?,?,?,?,?,?,?)`, newID("msg"), msg.ChannelID, msg.ProjectID, "human", mutation.ActorID, fmt.Sprintf("Approved %d ordered tasks for PRD %s.", len(tasks), p.Source.Path), marshalJSON(domain.CanonicalReferences{TaskIDs: p.TaskIDs}), nowMillis(), msg.ThreadID); err != nil {
		return nil, err
	}
	event, err := insertEventTx(ctx, tx, &publication.ID, nil, nil, "prd.tasks.approved", "PRD tasks approved", map[string]any{"project_id": msg.ProjectID, "thread_id": msg.ThreadID, "message_id": msg.ID, "task_ids": p.TaskIDs})
	if err != nil {
		return nil, err
	}
	if err = finishIdempotentMutation(ctx, tx, receipt, tasks); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.publish(event)
	return tasks, nil
}

func prdProgressTx(ctx context.Context, tx *sql.Tx, id string) (domain.PRDProgress, error) {
	publication, err := getTaskTx(ctx, tx, id)
	if err != nil {
		return domain.PRDProgress{}, err
	}
	if publication.PRD == nil {
		return domain.PRDProgress{}, domain.ErrNotFound
	}
	progress := domain.PRDProgress{Publication: publication, Plans: []domain.PRDTaskPlan{}, Tasks: []domain.Task{}, Acceptance: publication.PRDAcceptance}
	rows, err := tx.QueryContext(ctx, "SELECT prd_tasks_json FROM channel_messages WHERE project_id=? AND json_extract(prd_tasks_json,'$.publication_task_id')=? ORDER BY created_at,id", publication.ProjectID, id)
	if err != nil {
		return progress, err
	}
	for rows.Next() {
		var raw string
		var plan domain.PRDTaskPlan
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return progress, err
		}
		if err = json.Unmarshal([]byte(raw), &plan); err != nil {
			rows.Close()
			return progress, err
		}
		progress.Plans = append(progress.Plans, plan)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return progress, err
	}
	progress.ReadyToAccept = validatePRDLandingTx(ctx, tx, publication) == nil
	var approved []string
	type result struct {
		ID       string
		Version  int
		Criteria string
		Status   string
		Landing  *domain.TaskLanding
	}
	results := []result{}
	for _, p := range progress.Plans {
		if p.ApprovalDigest == "" {
			continue
		}
		approved = append(approved, p.Digest)
		for _, taskID := range p.TaskIDs {
			task, err := getTaskTx(ctx, tx, taskID)
			if err != nil {
				return progress, err
			}
			progress.Tasks = append(progress.Tasks, task)
			results = append(results, result{task.ID, task.Version, task.AcceptanceCriteria, task.Status, task.Landing})
			if validatePRDLandingTx(ctx, tx, task) != nil || task.PRDReference == nil || task.PRDReference.Digest != publication.PRD.Digest || task.Landing.Repository != publication.PRD.Repository.Repository || task.Landing.BaseBranch != publication.PRD.Repository.BaseBranch {
				progress.ReadyToAccept = false
			}
		}
	}
	if len(progress.Tasks) == 0 {
		progress.ReadyToAccept = false
	}
	progress.ResultsDigest = jsonDigest(struct {
		Publication string
		Source      string
		Plans       []string
		Results     []result
	}{publication.ID, publication.PRD.Digest, approved, results})
	return progress, nil
}
func (s *Store) GetPRDProgress(ctx context.Context, id string) (domain.PRDProgress, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PRDProgress{}, err
	}
	defer tx.Rollback()
	return prdProgressTx(ctx, tx, id)
}
func (s *Store) AcceptPRD(ctx context.Context, id string, mutation Mutation, digest string) (domain.PRDProgress, error) {
	if !strings.HasPrefix(mutation.ActorID, "operator:") || len(digest) != 64 {
		return domain.PRDProgress{}, domain.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PRDProgress{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE tasks SET id=id WHERE id=?", id); err != nil {
		return domain.PRDProgress{}, err
	}
	progress, err := prdProgressTx(ctx, tx, id)
	if err != nil {
		return progress, err
	}
	receipt, replay, exists, err := beginIdempotentMutation(ctx, tx, mutation, progress.Publication.ProjectID, "prd.accept", id, map[string]string{"results_digest": digest})
	if err != nil {
		return progress, err
	}
	if exists {
		err = decodeIdempotencyResult(replay, &progress)
		return progress, err
	}
	if !progress.ReadyToAccept || progress.ResultsDigest != digest {
		return progress, fmt.Errorf("all approved PRD tasks need current landing evidence: %w", domain.ErrConflict)
	}
	if progress.Acceptance == nil || progress.Acceptance.ResultsDigest != digest {
		progress.Acceptance = &domain.PRDAcceptance{ResultsDigest: digest, AcceptedBy: mutation.ActorID, AcceptedAt: nowMillis()}
		if _, err = tx.ExecContext(ctx, "UPDATE tasks SET prd_acceptance_json=? WHERE id=?", marshalJSON(progress.Acceptance), id); err != nil {
			return progress, err
		}
		var channel, thread string
		if err = tx.QueryRowContext(ctx, "SELECT m.channel_id,tt.thread_id FROM tasks t JOIN channel_messages m ON m.id=json_extract(t.prd_json,'$.message_id') JOIN thread_tasks tt ON tt.task_id=t.id WHERE t.id=?", id).Scan(&channel, &thread); err != nil {
			return progress, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO channel_messages(id,channel_id,project_id,author_type,author_id,body,references_json,created_at,thread_id) VALUES(?,?,?,?,?,?,?,?,?)`, newID("msg"), channel, progress.Publication.ProjectID, "human", mutation.ActorID, "Accepted PRD: "+progress.Publication.PRD.Title, marshalJSON(domain.CanonicalReferences{TaskIDs: []string{id}}), nowMillis(), thread); err != nil {
			return progress, err
		}
	}
	progress.Publication.PRDAcceptance = progress.Acceptance
	event, err := insertEventTx(ctx, tx, &id, nil, nil, "prd.accepted", "PRD accepted by operator", map[string]any{"project_id": progress.Publication.ProjectID, "results_digest": digest})
	if err != nil {
		return progress, err
	}
	if err = finishIdempotentMutation(ctx, tx, receipt, progress); err != nil {
		return progress, err
	}
	if err = tx.Commit(); err != nil {
		return progress, err
	}
	s.publish(event)
	return progress, nil
}

func validateImportedPRDTasks(ctx context.Context, tx *sql.Tx, bundle portableBundle) error {
	for _, msg := range bundle.ChannelMessages {
		p := msg.PRDTasks
		if p == nil {
			continue
		}
		publication, err := getTaskTx(ctx, tx, p.PublicationTaskID)
		if err != nil || p.Validate() != nil || p.Digest != p.ContentDigest() || p.Source.PlanMessageID != msg.ID || p.Source.PublicationTaskID != p.PublicationTaskID || msg.AuthorType != "agent" || msg.ThreadID == "" || msg.PRD != nil || publication.ProjectID != msg.ProjectID || publication.PRD == nil || publication.Landing == nil || publication.Landing.Commit != p.Revision || p.Source.Revision != p.Revision || publication.PRD.Path != p.Source.Path || publication.PRD.Digest != p.Source.Digest || publication.PRD.Repository != p.Source.Repository {
			return fmt.Errorf("invalid imported PRD task source: %w", domain.ErrInvalidInput)
		}
		var thread string
		if err = tx.QueryRowContext(ctx, "SELECT thread_id FROM thread_tasks WHERE task_id=?", publication.ID).Scan(&thread); err != nil || thread != msg.ThreadID {
			return domain.ErrInvalidInput
		}
		if p.ApprovalDigest == "" {
			if p.ApprovedAt != 0 || p.ApprovedBy != "" || len(p.TaskIDs) != 0 {
				return domain.ErrInvalidInput
			}
			continue
		}
		if len(p.ApprovalDigest) != 64 || !strings.HasPrefix(p.ApprovedBy, "operator:") || p.ApprovedAt <= 0 || len(p.TaskIDs) != len(p.Tasks) {
			return domain.ErrInvalidInput
		}
		seen := map[string]bool{}
		for _, id := range p.TaskIDs {
			task, err := getTaskTx(ctx, tx, id)
			if err != nil || seen[id] || task.PRD != nil || task.PRDReference == nil || *task.PRDReference != p.Source || task.ProjectID != msg.ProjectID || task.Context == nil || task.Context.Repository == nil || *task.Context.Repository != p.Source.Repository || task.Workload == nil || task.Workload.ID != "repository/v2" {
				return fmt.Errorf("invalid imported PRD task approval: %w", domain.ErrInvalidInput)
			}
			if err = tx.QueryRowContext(ctx, "SELECT thread_id FROM thread_tasks WHERE task_id=?", id).Scan(&thread); err != nil || thread != msg.ThreadID {
				return domain.ErrInvalidInput
			}
			seen[id] = true
		}
	}
	for _, item := range bundle.Tasks {
		task := item.Task
		if ref := task.PRDReference; ref != nil {
			msg, err := scanChannelMessage(tx.QueryRowContext(ctx, "SELECT "+channelMessageColumns+" FROM channel_messages WHERE id=?", ref.PlanMessageID))
			if err != nil || msg.PRDTasks == nil || msg.PRDTasks.ApprovalDigest == "" {
				return domain.ErrInvalidInput
			}
			found := false
			for _, id := range msg.PRDTasks.TaskIDs {
				if id == task.ID {
					found = true
				}
			}
			if !found {
				return domain.ErrInvalidInput
			}
		}
		if acceptance := task.PRDAcceptance; acceptance != nil {
			if task.PRD == nil || !strings.HasPrefix(acceptance.AcceptedBy, "operator:") || acceptance.AcceptedAt <= 0 || len(acceptance.ResultsDigest) != 64 {
				return domain.ErrInvalidInput
			}
			progress, err := prdProgressTx(ctx, tx, task.ID)
			if err != nil {
				return domain.ErrInvalidInput
			}
			if progress.ResultsDigest == acceptance.ResultsDigest {
				if len(progress.Tasks) == 0 || validatePRDLandingAtTx(ctx, tx, progress.Publication, acceptance.AcceptedAt) != nil {
					return domain.ErrInvalidInput
				}
				for _, derived := range progress.Tasks {
					if validatePRDLandingAtTx(ctx, tx, derived, acceptance.AcceptedAt) != nil {
						return fmt.Errorf("imported PRD acceptance lacks landed results: %w", domain.ErrInvalidInput)
					}
				}
			}
			// Historical acceptance remains history when later follow-up work changes
			// the result set. The browser only presents it as current when digests match.
		}
	}
	return nil
}
