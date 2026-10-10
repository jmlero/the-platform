package domain

// ConversationQuestion belongs to one invoked human message and its selected
// agent. Its answer is a canonical human reply in the same thread.
type ConversationQuestion struct {
	ID                  string   `json:"id"`
	ProjectID           string   `json:"project_id"`
	ChannelID           string   `json:"channel_id"`
	ThreadID            string   `json:"thread_id"`
	InvocationMessageID string   `json:"invocation_message_id"`
	AgentID             string   `json:"agent_id"`
	QuestionID          string   `json:"question_id"`
	MessageID           string   `json:"message_id"`
	Text                string   `json:"text"`
	Choices             []string `json:"choices"`
	Status              string   `json:"status"`
	AnswerMessageID     string   `json:"answer_message_id,omitempty"`
	Answer              string   `json:"answer,omitempty"`
	AnswerRequestID     string   `json:"answer_request_id,omitempty"`
	AnsweredBy          string   `json:"answered_by,omitempty"`
	CreatedAt           int64    `json:"created_at"`
	AnsweredAt          int64    `json:"answered_at,omitempty"`
	ExpiresAt           int64    `json:"expires_at,omitempty"`
	ClosedAt            int64    `json:"closed_at,omitempty"`
}

type ConversationQuestionInput struct {
	InvocationMessageID string   `json:"invocation_message_id"`
	AgentID             string   `json:"agent_id"`
	QuestionID          string   `json:"question_id"`
	Text                string   `json:"text"`
	Choices             []string `json:"choices,omitempty"`
	ExpiresAt           int64    `json:"expires_at,omitempty"`
}
