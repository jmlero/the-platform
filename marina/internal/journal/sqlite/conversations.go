package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type ConversationTurn struct {
	MessageID, ProjectID, ChannelID, ThreadID, AgentID, RequestID, Body string
	Status, SpecificationDigest, ResultDigest, Reason, ModelCallStatus  string
	PublishedMessageID, PublishedTaskID                                 string
	PublicationJSON                                                     string
	Specification                                                       rt.Specification
	DeadlineAt                                                          time.Time
	MaxTokens, ReservedTokens, UsedTokens                               int64
	CreatedAt, UpdatedAt                                                int64
}

func scanConversationTurn(row interface{ Scan(...any) error }) (ConversationTurn, error) {
	var turn ConversationTurn
	var spec string
	var deadline int64
	err := row.Scan(&turn.MessageID, &turn.ProjectID, &turn.ChannelID, &turn.ThreadID, &turn.AgentID, &turn.RequestID,
		&turn.Body, &turn.Status, &spec, &turn.SpecificationDigest, &turn.ResultDigest, &turn.Reason, &deadline,
		&turn.MaxTokens, &turn.ReservedTokens, &turn.UsedTokens, &turn.ModelCallStatus, &turn.CreatedAt, &turn.UpdatedAt, &turn.PublishedMessageID, &turn.PublishedTaskID, &turn.PublicationJSON)
	if err != nil {
		return turn, wrapNotFound(err)
	}
	turn.DeadlineAt = time.UnixMilli(deadline).UTC()
	if spec != "" {
		err = json.Unmarshal([]byte(spec), &turn.Specification)
	}
	return turn, err
}

const conversationColumns = `message_id,project_id,channel_id,thread_id,agent_id,request_id,body,status,specification_json,specification_digest,result_digest,reason,deadline_at,max_tokens,reserved_tokens,used_tokens,model_call_status,created_at,updated_at,published_message_id,published_task_id,publication_json`

func (s *Store) GetConversationTurn(ctx context.Context, messageID string) (ConversationTurn, error) {
	return scanConversationTurn(s.db.QueryRowContext(ctx, "SELECT "+conversationColumns+" FROM conversation_turns WHERE message_id=?", messageID))
}

// A prepared template carries one attempt's gateway credential. Never assign
// it to another invocation, including after the first turn is complete.
func (s *Store) ConversationTemplateOwner(ctx context.Context, namespace, template string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT message_id FROM conversation_turns
		WHERE json_extract(CASE WHEN json_valid(specification_json) THEN specification_json ELSE '{}' END,'$.engine.namespace')=?
		AND json_extract(CASE WHEN json_valid(specification_json) THEN specification_json ELSE '{}' END,'$.engine.template')=?
		ORDER BY created_at LIMIT 1`, namespace, template).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Store) EnsureConversationTurn(ctx context.Context, message workcontrol.ConversationMessage, deadline time.Time, maxTokens int64) (ConversationTurn, error) {
	if message.ID == "" || message.ProjectID == "" || message.ChannelID == "" || message.ThreadID == "" ||
		message.AuthorType != "human" || message.InvokedAgentID == "" || message.InvocationRequestID == "" || message.Body == "" ||
		len(message.Body) > 16<<10 || maxTokens < 1 || deadline.IsZero() {
		return ConversationTurn{}, errors.New("invalid bounded conversation invocation")
	}
	now := s.clock().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversation_turns(message_id,project_id,channel_id,thread_id,agent_id,request_id,body,status,deadline_at,max_tokens,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,'pending',?,?,?,?) ON CONFLICT(message_id) DO NOTHING`,
		message.ID, message.ProjectID, message.ChannelID, message.ThreadID, message.InvokedAgentID, message.InvocationRequestID,
		message.Body, deadline.UnixMilli(), maxTokens, now, now)
	if err != nil {
		return ConversationTurn{}, err
	}
	turn, err := s.GetConversationTurn(ctx, message.ID)
	if err != nil {
		return turn, err
	}
	if turn.ProjectID != message.ProjectID || turn.ChannelID != message.ChannelID || turn.ThreadID != message.ThreadID ||
		turn.AgentID != message.InvokedAgentID || turn.RequestID != message.InvocationRequestID || turn.Body != message.Body {
		return ConversationTurn{}, errors.New("conversation invocation changed after recording")
	}
	return turn, nil
}

func (s *Store) ListActiveConversationTurns(ctx context.Context, projectID, agentID string, limit int) ([]ConversationTurn, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+conversationColumns+" FROM conversation_turns WHERE project_id=? AND agent_id=? AND status IN ('pending','running') ORDER BY created_at,message_id LIMIT ?", projectID, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var turns []ConversationTurn
	for rows.Next() {
		turn, err := scanConversationTurn(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	return turns, rows.Err()
}

func (s *Store) ListUnpublishedConversationTurns(ctx context.Context, projectID, agentID string, limit int) ([]ConversationTurn, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+conversationColumns+" FROM conversation_turns WHERE project_id=? AND agent_id=? AND status IN ('completed','blocked') AND published_message_id='' ORDER BY created_at,message_id LIMIT ?", projectID, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var turns []ConversationTurn
	for rows.Next() {
		turn, err := scanConversationTurn(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	return turns, rows.Err()
}

func (s *Store) MarkConversationPublished(ctx context.Context, messageID, publishedMessageID, taskID string) error {
	if messageID == "" || publishedMessageID == "" {
		return errors.New("conversation publication requires canonical identities")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE conversation_turns SET published_message_id=?,published_task_id=?,updated_at=? WHERE message_id=? AND status IN ('completed','blocked') AND published_message_id=''`, publishedMessageID, taskID, s.clock().UnixMilli(), messageID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}
	turn, err := s.GetConversationTurn(ctx, messageID)
	if err == nil && turn.PublishedMessageID == publishedMessageID && turn.PublishedTaskID == taskID {
		return nil
	}
	return errors.New("conversation publication changed on replay")
}

func (s *Store) SaveConversationPublication(ctx context.Context, messageID string, input workcontrol.ConversationOutcomeInput) error {
	if messageID == "" || input.InvocationMessageID != messageID {
		return errors.New("conversation publication origin mismatch")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE conversation_turns SET publication_json=?,updated_at=? WHERE message_id=? AND status IN ('completed','blocked') AND publication_json=''`, string(encoded), s.clock().UnixMilli(), messageID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}
	turn, err := s.GetConversationTurn(ctx, messageID)
	if err == nil && turn.PublicationJSON == string(encoded) {
		return nil
	}
	return errors.New("conversation publication intent changed")
}

func (s *Store) GetConversationDispatchCursor(ctx context.Context, projectID string) (int64, error) {
	var cursor int64
	err := s.db.QueryRowContext(ctx, `SELECT event_id FROM conversation_dispatch_cursors WHERE project_id=?`, projectID).Scan(&cursor)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return cursor, err
}

func (s *Store) SetConversationDispatchCursor(ctx context.Context, projectID string, eventID int64) error {
	if projectID == "" || eventID < 1 {
		return errors.New("conversation dispatch cursor requires a project and event")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversation_dispatch_cursors(project_id,event_id) VALUES(?,?)
		ON CONFLICT(project_id) DO UPDATE SET event_id=excluded.event_id WHERE excluded.event_id>conversation_dispatch_cursors.event_id`, projectID, eventID)
	return err
}

func (s *Store) SaveConversationSpecification(ctx context.Context, messageID string, spec rt.Specification) error {
	digest, err := spec.Digest()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE conversation_turns SET specification_json=?,specification_digest=?,status='running',updated_at=?
		WHERE message_id=? AND status='pending' AND specification_digest='' AND deadline_at=?`, string(encoded), digest, s.clock().UnixMilli(), messageID, spec.DeadlineAt.UnixMilli())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}
	turn, err := s.GetConversationTurn(ctx, messageID)
	if err == nil && turn.SpecificationDigest == digest && turn.Status == "running" {
		return nil
	}
	return fmt.Errorf("conversation specification cannot change after dispatch")
}

func (s *Store) FinishConversationTurn(ctx context.Context, messageID, status, resultDigest, reason string) error {
	if status != "completed" && status != "blocked" {
		return errors.New("invalid conversation terminal state")
	}
	if status == "completed" && resultDigest == "" || status == "blocked" && reason == "" {
		return errors.New("conversation terminal evidence is required")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE conversation_turns SET status=?,result_digest=?,reason=?,updated_at=? WHERE message_id=? AND status IN ('pending','running')`, status, resultDigest, reason, s.clock().UnixMilli(), messageID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}
	turn, err := s.GetConversationTurn(ctx, messageID)
	if err == nil && turn.Status == status && turn.ResultDigest == resultDigest && turn.Reason == reason {
		return nil
	}
	return errors.New("conversation terminal result changed")
}

// A turn permits one model call. An unknown response retains its reservation.
func (s *Store) ReserveConversationCall(ctx context.Context, messageID string, tokens int64) error {
	if tokens < 1 {
		return errors.New("invalid model reservation")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE conversation_turns SET reserved_tokens=?,model_call_status='reserved',updated_at=?
		WHERE message_id=? AND status='running' AND model_call_status='' AND used_tokens=0 AND ?<=max_tokens AND deadline_at>?`,
		tokens, s.clock().UnixMilli(), messageID, tokens, s.clock().UnixMilli())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("conversation model call unavailable or budget exhausted")
	}
	return nil
}

func (s *Store) CompleteConversationCall(ctx context.Context, messageID string, tokens int64) error {
	if tokens < 0 {
		return errors.New("negative model usage")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE conversation_turns SET reserved_tokens=0,used_tokens=?,model_call_status='completed',updated_at=?
		WHERE message_id=? AND model_call_status='reserved' AND ?<=reserved_tokens`, tokens, s.clock().UnixMilli(), messageID, tokens)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("conversation usage exceeds reservation or call is not pending")
	}
	return nil
}
