package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const conversationQuestionColumns = "id,project_id,channel_id,thread_id,invocation_message_id,agent_id,question_id,message_id,text,choices_json,status,answer_message_id,answer,answer_request_id,answered_by,created_at,answered_at"

func scanConversationQuestion(row interface{ Scan(...any) error }) (domain.ConversationQuestion, error) {
	var q domain.ConversationQuestion
	var choices string
	var answerMessageID, answerRequestID sql.NullString
	var answeredAt sql.NullInt64
	err := row.Scan(&q.ID, &q.ProjectID, &q.ChannelID, &q.ThreadID, &q.InvocationMessageID, &q.AgentID, &q.QuestionID, &q.MessageID, &q.Text, &choices, &q.Status, &answerMessageID, &q.Answer, &answerRequestID, &q.AnsweredBy, &q.CreatedAt, &answeredAt)
	if err != nil {
		return q, normalizeStoreError(err)
	}
	if err := json.Unmarshal([]byte(choices), &q.Choices); err != nil {
		return q, err
	}
	q.AnswerMessageID, q.AnswerRequestID, q.AnsweredAt = answerMessageID.String, answerRequestID.String, answeredAt.Int64
	return q, nil
}

func (s *Store) GetConversationQuestion(ctx context.Context, id string) (domain.ConversationQuestion, error) {
	return scanConversationQuestion(s.db.QueryRowContext(ctx, "SELECT "+conversationQuestionColumns+" FROM conversation_questions WHERE id=?", id))
}

func (s *Store) ListConversationQuestions(ctx context.Context, threadID string) ([]domain.ConversationQuestion, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+conversationQuestionColumns+" FROM conversation_questions WHERE thread_id=? ORDER BY created_at,id", threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.ConversationQuestion, 0)
	for rows.Next() {
		q, err := scanConversationQuestion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, q)
	}
	return items, rows.Err()
}

// EnsureConversationQuestion records the question and its visible channel reply
// atomically. The engine's question ID is scoped to one invocation.
func (s *Store) EnsureConversationQuestion(ctx context.Context, in domain.ConversationQuestionInput) (domain.ConversationQuestion, error) {
	in.InvocationMessageID = strings.TrimSpace(in.InvocationMessageID)
	in.AgentID = strings.TrimSpace(in.AgentID)
	in.QuestionID = strings.TrimSpace(in.QuestionID)
	in.Text = strings.TrimSpace(in.Text)
	if in.InvocationMessageID == "" || in.AgentID == "" || in.QuestionID == "" || len(in.QuestionID) > 160 || in.Text == "" || len(in.Text) > 2000 || len(in.Choices) > 10 {
		return domain.ConversationQuestion{}, fmt.Errorf("invalid conversation question: %w", domain.ErrInvalidInput)
	}
	for _, choice := range in.Choices {
		if strings.TrimSpace(choice) == "" || len(choice) > 200 {
			return domain.ConversationQuestion{}, fmt.Errorf("invalid question choice: %w", domain.ErrInvalidInput)
		}
	}
	if in.Choices == nil {
		in.Choices = []string{}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ConversationQuestion{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE channel_messages SET id=id WHERE id=?", in.InvocationMessageID); err != nil {
		return domain.ConversationQuestion{}, err
	}
	invocation, err := scanChannelMessage(tx.QueryRowContext(ctx, "SELECT "+channelMessageColumns+" FROM channel_messages WHERE id=?", in.InvocationMessageID))
	if err != nil {
		return domain.ConversationQuestion{}, err
	}
	if invocation.AuthorType != "human" || invocation.InvokedAgentID != in.AgentID || invocation.ThreadID == "" || invocation.InvocationRequestID == "" {
		return domain.ConversationQuestion{}, fmt.Errorf("question is not bound to an invoked agent: %w", domain.ErrConflict)
	}
	prior, err := scanConversationQuestion(tx.QueryRowContext(ctx, "SELECT "+conversationQuestionColumns+" FROM conversation_questions WHERE invocation_message_id=? AND question_id=?", in.InvocationMessageID, in.QuestionID))
	if err == nil {
		if prior.AgentID != in.AgentID || prior.Text != in.Text || marshalJSON(prior.Choices) != marshalJSON(in.Choices) {
			return domain.ConversationQuestion{}, fmt.Errorf("question replay changed: %w", domain.ErrConflict)
		}
		return prior, nil
	}
	if err != domain.ErrNotFound {
		return domain.ConversationQuestion{}, err
	}
	q := domain.ConversationQuestion{ID: newID("cq"), ProjectID: invocation.ProjectID, ChannelID: invocation.ChannelID, ThreadID: invocation.ThreadID, InvocationMessageID: invocation.ID, AgentID: in.AgentID, QuestionID: in.QuestionID, MessageID: newID("msg"), Text: in.Text, Choices: in.Choices, Status: "pending", CreatedAt: nowMillis()}
	if q.Choices == nil {
		q.Choices = []string{}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO channel_messages ("+channelMessageColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?)", q.MessageID, q.ChannelID, q.ProjectID, "agent", q.AgentID, q.Text, "{}", nil, q.CreatedAt, q.ThreadID, "", nil); err != nil {
		return domain.ConversationQuestion{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO conversation_questions ("+conversationQuestionColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", q.ID, q.ProjectID, q.ChannelID, q.ThreadID, q.InvocationMessageID, q.AgentID, q.QuestionID, q.MessageID, q.Text, marshalJSON(q.Choices), q.Status, nil, "", nil, "", q.CreatedAt, nil); err != nil {
		return domain.ConversationQuestion{}, err
	}
	event, err := insertEventTx(ctx, tx, nil, nil, nil, "conversation.question.created", "Conversation question asked", map[string]any{"project_id": q.ProjectID, "channel_id": q.ChannelID, "thread_id": q.ThreadID, "question_id": q.ID, "message_id": q.MessageID})
	if err != nil {
		return domain.ConversationQuestion{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ConversationQuestion{}, err
	}
	s.publish(event)
	return q, nil
}

func (s *Store) AnswerConversationQuestion(ctx context.Context, id, answer, authorID, requestID string) (domain.ConversationQuestion, error) {
	answer, authorID, requestID = strings.TrimSpace(answer), strings.TrimSpace(authorID), strings.TrimSpace(requestID)
	if id == "" || answer == "" || len(answer) > 2000 || authorID == "" || requestID == "" || len(requestID) > 128 {
		return domain.ConversationQuestion{}, fmt.Errorf("invalid conversation answer: %w", domain.ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ConversationQuestion{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE conversation_questions SET id=id WHERE id=?", id); err != nil {
		return domain.ConversationQuestion{}, err
	}
	q, err := scanConversationQuestion(tx.QueryRowContext(ctx, "SELECT "+conversationQuestionColumns+" FROM conversation_questions WHERE id=?", id))
	if err != nil {
		return domain.ConversationQuestion{}, err
	}
	if q.Status == "answered" {
		if q.Answer == answer && q.AnsweredBy == authorID && q.AnswerRequestID == requestID {
			return q, nil
		}
		return domain.ConversationQuestion{}, fmt.Errorf("question already answered: %w", domain.ErrConflict)
	}
	var usedID string
	err = tx.QueryRowContext(ctx, "SELECT id FROM conversation_questions WHERE answer_request_id=?", requestID).Scan(&usedID)
	if err != nil && err != sql.ErrNoRows {
		return domain.ConversationQuestion{}, err
	}
	if err == nil {
		return domain.ConversationQuestion{}, fmt.Errorf("answer request already used: %w", domain.ErrConflict)
	}
	q.AnswerMessageID, q.Answer, q.AnswerRequestID, q.AnsweredBy, q.AnsweredAt, q.Status = newID("msg"), answer, requestID, authorID, nowMillis(), "answered"
	if _, err := tx.ExecContext(ctx, "INSERT INTO channel_messages ("+channelMessageColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?)", q.AnswerMessageID, q.ChannelID, q.ProjectID, "human", authorID, answer, "{}", nil, q.AnsweredAt, q.ThreadID, "", nil); err != nil {
		return domain.ConversationQuestion{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE conversation_questions SET status='answered',answer_message_id=?,answer=?,answer_request_id=?,answered_by=?,answered_at=? WHERE id=? AND status='pending'", q.AnswerMessageID, answer, requestID, authorID, q.AnsweredAt, q.ID); err != nil {
		return domain.ConversationQuestion{}, err
	}
	event, err := insertEventTx(ctx, tx, nil, nil, nil, "conversation.question.answered", "Conversation question answered", map[string]any{"project_id": q.ProjectID, "channel_id": q.ChannelID, "thread_id": q.ThreadID, "question_id": q.ID, "message_id": q.AnswerMessageID})
	if err != nil {
		return domain.ConversationQuestion{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ConversationQuestion{}, err
	}
	s.publish(event)
	return q, nil
}
