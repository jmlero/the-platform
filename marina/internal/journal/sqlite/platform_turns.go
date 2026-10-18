package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

// Turns: a client freezes a thread and its context into a Turn resource; the
// conversation workflow reads it through the same invocation feed it used
// before. Each Turn is its own channel, and its last message is the invoked
// one, identified by the Turn itself.

// TurnMessage is one message of a Turn's frozen thread.
type TurnMessage struct {
	ID, Role, Body string
}

// NewPlatformTurn is a Turn accepted from a client.
type NewPlatformTurn struct {
	Namespace, Name, UID, ProjectID, AgentID, ThreadID string
	Messages                                           []TurnMessage
	Context                                            workcontrol.ResolvedContext
}

type platformTurnSpec struct {
	AgentID, ThreadID string
	Messages          []TurnMessage
	Context           workcontrol.ResolvedContext
}

// PlatformTurn is one Turn as the ledger holds it.
type PlatformTurn struct {
	Seq                                                     int64
	ID, Namespace, Name, UID, ProjectID, SpecDigest, Status string
	AgentID, ThreadID                                       string
	Messages                                                []TurnMessage
	Context                                                 workcontrol.ResolvedContext
	Outcome                                                 *workcontrol.ConversationOutcomeInput
	CancelRequested                                         bool
	CreatedAt, UpdatedAt                                    time.Time
}

func turnChannel(id string) string { return "turn:" + id }

func (p *Platform) SubmitTurn(ctx context.Context, input NewPlatformTurn) (PlatformTurn, bool, error) {
	if input.Namespace == "" || input.Name == "" || input.UID == "" || input.ProjectID == "" || input.AgentID == "" || input.ThreadID == "" || len(input.Messages) == 0 {
		return PlatformTurn{}, false, invalid("turn namespace, name, uid, project, agent, thread and messages are required")
	}
	if last := input.Messages[len(input.Messages)-1]; last.Role != "human" || strings.TrimSpace(last.Body) == "" {
		return PlatformTurn{}, false, invalid("a turn's last message must be a human message")
	}
	id := PlatformWorkID(input.Namespace, input.Name)
	spec := platformTurnSpec{AgentID: input.AgentID, ThreadID: input.ThreadID, Messages: input.Messages, Context: input.Context}
	digest := digestJSON(spec)
	existing, err := p.Turn(ctx, id)
	if err == nil {
		if existing.SpecDigest != digest || existing.UID != input.UID {
			return existing, false, conflict("turn %s already exists with a different spec", input.Name)
		}
		return existing, false, nil
	}
	if !isPlatformNotFound(err) {
		return PlatformTurn{}, false, err
	}
	encoded, _ := json.Marshal(spec)
	now := millis(p.store.now())
	if _, err := p.store.db.ExecContext(ctx, `INSERT INTO platform_turns (id, namespace, name, uid, project_id, spec_digest, spec_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`, id, input.Namespace, input.Name, input.UID, input.ProjectID, digest, string(encoded), now, now); err != nil {
		return PlatformTurn{}, false, err
	}
	if err := p.appendEvent(ctx, p.store.db, id, "turn.accepted", "Turn accepted", nil, ""); err != nil {
		return PlatformTurn{}, false, err
	}
	created, err := p.Turn(ctx, id)
	return created, err == nil, err
}

const turnColumns = "seq, id, namespace, name, uid, project_id, spec_digest, spec_json, status, outcome_json, cancel_requested, created_at, updated_at"

func scanTurn(row scanner) (PlatformTurn, error) {
	var t PlatformTurn
	var spec, outcome string
	var cancel int
	var created, updated int64
	if err := row.Scan(&t.Seq, &t.ID, &t.Namespace, &t.Name, &t.UID, &t.ProjectID, &t.SpecDigest, &spec, &t.Status, &outcome, &cancel, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PlatformTurn{}, notFound("turn")
		}
		return PlatformTurn{}, err
	}
	var decoded platformTurnSpec
	if err := json.Unmarshal([]byte(spec), &decoded); err != nil {
		return PlatformTurn{}, err
	}
	t.AgentID, t.ThreadID, t.Messages, t.Context = decoded.AgentID, decoded.ThreadID, decoded.Messages, decoded.Context
	if outcome != "" {
		t.Outcome = &workcontrol.ConversationOutcomeInput{}
		if err := json.Unmarshal([]byte(outcome), t.Outcome); err != nil {
			return PlatformTurn{}, err
		}
	}
	t.CancelRequested, t.CreatedAt, t.UpdatedAt = cancel == 1, fromMillis(created), fromMillis(updated)
	return t, nil
}

func (p *Platform) Turn(ctx context.Context, id string) (PlatformTurn, error) {
	return scanTurn(p.store.db.QueryRowContext(ctx, "SELECT "+turnColumns+" FROM platform_turns WHERE id = ?", id))
}

// ListTurns returns Turns in creation order.
func (p *Platform) ListTurns(ctx context.Context, activeOnly bool, limit int) ([]PlatformTurn, error) {
	query := "SELECT " + turnColumns + " FROM platform_turns"
	if activeOnly {
		query += " WHERE status = 'pending'"
	}
	rows, err := p.store.db.QueryContext(ctx, query+" ORDER BY seq LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlatformTurn
	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (t PlatformTurn) message(index int) workcontrol.ConversationMessage {
	m := t.Messages[index]
	id := m.ID
	if index == len(t.Messages)-1 {
		id = t.ID
	}
	author := m.Role
	message := workcontrol.ConversationMessage{ID: id, ChannelID: turnChannel(t.ID), ProjectID: t.ProjectID, ThreadID: t.ThreadID, AuthorType: author,
		Body: m.Body, References: workcontrol.ConversationReferences{TaskIDs: []string{}}, CreatedAt: t.CreatedAt.UnixMilli()}
	if index == len(t.Messages)-1 {
		message.InvokedAgentID, message.InvocationRequestID = t.AgentID, t.UID
	}
	return message
}

// ListConversationInvocations is the ordered feed of Turns for one project.
func (p *Platform) ListConversationInvocations(ctx context.Context, projectID string, afterID int64) ([]workcontrol.ConversationInvocation, error) {
	rows, err := p.store.db.QueryContext(ctx, "SELECT "+turnColumns+" FROM platform_turns WHERE project_id = ? AND seq > ? AND cancel_requested = 0 ORDER BY seq LIMIT 50", projectID, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workcontrol.ConversationInvocation
	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, workcontrol.ConversationInvocation{EventID: t.Seq, InvokedAt: t.CreatedAt.UnixMilli(), Message: t.message(len(t.Messages) - 1)})
	}
	return out, rows.Err()
}

func (p *Platform) PageConversationMessages(ctx context.Context, channelID, _ string) (workcontrol.ConversationMessagePage, error) {
	id, ok := strings.CutPrefix(channelID, "turn:")
	if !ok {
		return workcontrol.ConversationMessagePage{}, notFound("channel")
	}
	t, err := p.Turn(ctx, id)
	if err != nil {
		return workcontrol.ConversationMessagePage{}, err
	}
	page := workcontrol.ConversationMessagePage{Items: []workcontrol.ConversationMessage{}}
	for i := range t.Messages {
		page.Items = append(page.Items, t.message(i))
	}
	return page, nil
}

func (p *Platform) GetConversationContext(ctx context.Context, invocationMessageID string) (workcontrol.ResolvedContext, error) {
	t, err := p.Turn(ctx, invocationMessageID)
	if err != nil {
		return workcontrol.ResolvedContext{}, err
	}
	resolved := t.Context
	if resolved.Sources == nil {
		resolved.Sources = []workcontrol.ContextSource{}
	}
	if resolved.Messages == nil {
		resolved.Messages = []workcontrol.ContextMessage{}
	}
	return resolved, nil
}

// EnsureConversationQuestion creates the Turn's clarification Decision once
// and reports its current state.
func (p *Platform) EnsureConversationQuestion(ctx context.Context, input workcontrol.ConversationQuestionInput) (workcontrol.ConversationQuestion, error) {
	t, err := p.Turn(ctx, input.InvocationMessageID)
	if err != nil {
		return workcontrol.ConversationQuestion{}, err
	}
	if input.AgentID != t.AgentID || input.QuestionID == "" || strings.TrimSpace(input.Text) == "" || len(input.Choices) > 6 {
		return workcontrol.ConversationQuestion{}, invalid("a question needs the turn's agent, an identity, text and at most six choices")
	}
	name := t.Name + "-question-" + shortDigest(input.QuestionID)
	d, err := p.FindDecision(ctx, t.Namespace, name)
	if isPlatformNotFound(err) {
		tx, txErr := p.store.db.BeginTx(ctx, nil)
		if txErr != nil {
			return workcontrol.ConversationQuestion{}, txErr
		}
		defer tx.Rollback()
		expires := time.UnixMilli(input.ExpiresAt).UTC()
		mode := "choice"
		if len(input.Choices) == 0 {
			mode = "text"
		}
		decision := workcontrol.Decision{TaskID: t.ID, InteractionID: input.QuestionID, AnswerMode: mode, Question: input.Text, Options: input.Choices}
		if input.ExpiresAt > 0 {
			decision.ExpiresAt = &expires
		}
		if _, err := p.insertDecision(ctx, tx, t.Namespace, name, "Turn", t.ID, decision); err != nil {
			return workcontrol.ConversationQuestion{}, err
		}
		if err := tx.Commit(); err != nil {
			return workcontrol.ConversationQuestion{}, err
		}
		d, err = p.FindDecision(ctx, t.Namespace, name)
	}
	if err != nil {
		return workcontrol.ConversationQuestion{}, err
	}
	if d.Decision.Question != input.Text {
		return workcontrol.ConversationQuestion{}, conflict("question %s changed", input.QuestionID)
	}
	q := workcontrol.ConversationQuestion{ID: d.Decision.ID, ProjectID: t.ProjectID, ChannelID: turnChannel(t.ID), ThreadID: t.ThreadID,
		InvocationMessageID: t.ID, AgentID: t.AgentID, QuestionID: input.QuestionID, Status: "pending", Answer: d.Decision.Answer}
	if d.Decision.ExpiresAt != nil {
		q.ExpiresAt = d.Decision.ExpiresAt.UnixMilli()
	}
	switch d.Decision.Status {
	case workcontrol.DecisionAnswered:
		q.Status, q.AnswerMessageID = "answered", d.Decision.ID+".answer"
	case workcontrol.DecisionPending:
	default:
		q.Status, q.ClosedAt = string(d.Decision.Status), p.store.now().UnixMilli()
	}
	return q, nil
}

// PublishConversationOutcome records a Turn's reply and proposals once. A
// task the turn proposes gets a stable proposal identity; the client decides
// whether to create a Run from it.
func (p *Platform) PublishConversationOutcome(ctx context.Context, input workcontrol.ConversationOutcomeInput) (workcontrol.ConversationOutcome, error) {
	t, err := p.Turn(ctx, input.InvocationMessageID)
	if err != nil {
		return workcontrol.ConversationOutcome{}, err
	}
	if input.AgentID != t.AgentID || input.Status == "" || input.ResultDigest == "" {
		return workcontrol.ConversationOutcome{}, invalid("an outcome needs the turn's agent, a status and a result digest")
	}
	out := workcontrol.ConversationOutcome{InvocationMessageID: t.ID, MessageID: t.ID + ".reply", Status: input.Status, ResultDigest: input.ResultDigest, FollowupTaskID: input.FollowupTaskID}
	if input.Task != nil {
		out.TaskID = t.ID + ".task"
	}
	if t.Outcome != nil {
		if digestJSON(t.Outcome) != digestJSON(input) {
			return out, conflict("turn %s already has a different outcome", t.Name)
		}
		return out, nil
	}
	encoded, _ := json.Marshal(input)
	status := "completed"
	if input.Status != "completed" && input.Status != "answered" {
		status = "failed"
	}
	now := millis(p.store.now())
	result, err := p.store.db.ExecContext(ctx, "UPDATE platform_turns SET outcome_json = ?, status = ?, updated_at = ? WHERE id = ? AND outcome_json = ''", string(encoded), status, now, t.ID)
	if err != nil {
		return out, err
	}
	if err := requireOneMutation(result); err != nil {
		return p.PublishConversationOutcome(ctx, input)
	}
	if _, err := p.store.db.ExecContext(ctx, "UPDATE platform_decisions SET status = 'closed', updated_at = ? WHERE owner_id = ? AND status = 'pending'", now, t.ID); err != nil {
		return out, err
	}
	return out, p.appendEvent(ctx, p.store.db, t.ID, "turn."+status, "Turn "+status, nil, "")
}

// RequestTurnCancel stops a Turn that has not produced an outcome.
func (p *Platform) RequestTurnCancel(ctx context.Context, id, requestedBy string) error {
	result, err := p.store.db.ExecContext(ctx, "UPDATE platform_turns SET cancel_requested = 1, updated_at = ? WHERE id = ? AND cancel_requested = 0", millis(p.store.now()), id)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 1 {
		return p.appendEvent(ctx, p.store.db, id, "turn.cancel_requested", "Cancellation requested", map[string]any{"by": requestedBy}, "")
	}
	return nil
}

// TurnCancelRequested reports whether the client cancelled a Turn.
func (p *Platform) TurnCancelRequested(ctx context.Context, id string) (bool, error) {
	t, err := p.Turn(ctx, id)
	if isPlatformNotFound(err) {
		return false, nil
	}
	return t.CancelRequested, err
}
