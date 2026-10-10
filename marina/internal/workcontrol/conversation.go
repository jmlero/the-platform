package workcontrol

import "context"

// ConversationSource exposes operator-authored invocations through Deck's
// canonical channel ledger. Messages are immutable once invoked.
type ConversationSource interface {
	ListConversationInvocations(context.Context, string, int64) ([]ConversationInvocation, error)
	PageConversationMessages(context.Context, string, string) (ConversationMessagePage, error)
	EnsureConversationQuestion(context.Context, ConversationQuestionInput) (ConversationQuestion, error)
	PublishConversationOutcome(context.Context, ConversationOutcomeInput) (ConversationOutcome, error)
}

type ConversationBudget struct {
	TimeMinutes    int      `json:"time_minutes"`
	MaxAttempts    int      `json:"max_attempts"`
	MaxTokens      int64    `json:"max_tokens"`
	MaxCostMinor   int64    `json:"max_cost_minor"`
	Currency       string   `json:"currency"`
	MaxConcurrency int      `json:"max_concurrency"`
	Authority      []string `json:"authority"`
}

type ConversationTaskInput struct {
	Title              string              `json:"title"`
	Description        string              `json:"description"`
	AcceptanceCriteria string              `json:"acceptance_criteria"`
	Status             string              `json:"status"`
	Budget             *ConversationBudget `json:"budget,omitempty"`
}

type ConversationOutcomeInput struct {
	PRDTasks            *PRDTaskProposal       `json:"prd_tasks,omitempty"`
	PRD                 *PRDDraft              `json:"prd,omitempty"`
	InvocationMessageID string                 `json:"invocation_message_id"`
	AgentID             string                 `json:"agent_id"`
	Status              string                 `json:"status"`
	Body                string                 `json:"body"`
	ResultDigest        string                 `json:"result_digest"`
	ProfileID           string                 `json:"profile_id,omitempty"`
	ProfileDigest       string                 `json:"profile_digest,omitempty"`
	Task                *ConversationTaskInput `json:"task,omitempty"`
	FollowupTaskID      string                 `json:"followup_task_id,omitempty"`
}

type ConversationOutcome struct {
	InvocationMessageID string `json:"invocation_message_id"`
	MessageID           string `json:"message_id"`
	TaskID              string `json:"task_id"`
	FollowupTaskID      string `json:"followup_task_id"`
	Status              string `json:"status"`
	ResultDigest        string `json:"result_digest"`
}

type ConversationReferences struct {
	TaskIDs []string `json:"task_ids"`
}

type ConversationQuestionInput struct {
	InvocationMessageID string   `json:"invocation_message_id"`
	AgentID             string   `json:"agent_id"`
	QuestionID          string   `json:"question_id"`
	Text                string   `json:"text"`
	Choices             []string `json:"choices,omitempty"`
	ExpiresAt           int64    `json:"expires_at,omitempty"`
}

type ConversationQuestion struct {
	ID                  string `json:"id"`
	ProjectID           string `json:"project_id"`
	ChannelID           string `json:"channel_id"`
	ThreadID            string `json:"thread_id"`
	InvocationMessageID string `json:"invocation_message_id"`
	AgentID             string `json:"agent_id"`
	QuestionID          string `json:"question_id"`
	Status              string `json:"status"`
	AnswerMessageID     string `json:"answer_message_id"`
	Answer              string `json:"answer"`
	ExpiresAt           int64  `json:"expires_at,omitempty"`
	ClosedAt            int64  `json:"closed_at,omitempty"`
}

type ConversationInvocation struct {
	EventID   int64               `json:"event_id"`
	InvokedAt int64               `json:"invoked_at"`
	Message   ConversationMessage `json:"message"`
}

type ConversationMessage struct {
	PRDTasks            *PRDTaskPlan           `json:"prd_tasks,omitempty"`
	PromotedTaskID      *string                `json:"promoted_task_id,omitempty"`
	PRD                 *PRDDocument           `json:"prd,omitempty"`
	References          ConversationReferences `json:"references"`
	ID                  string                 `json:"id"`
	ChannelID           string                 `json:"channel_id"`
	ProjectID           string                 `json:"project_id"`
	ThreadID            string                 `json:"thread_id"`
	AuthorType          string                 `json:"author_type"`
	Body                string                 `json:"body"`
	InvokedAgentID      string                 `json:"invoked_agent_id"`
	InvocationRequestID string                 `json:"invocation_request_id"`
	CreatedAt           int64                  `json:"created_at"`
}

type ConversationMessagePage struct {
	Items      []ConversationMessage `json:"items"`
	NextCursor string                `json:"next_cursor"`
}
