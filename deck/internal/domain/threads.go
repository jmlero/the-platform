package domain

// ConversationThread references canonical messages and tasks, not copies of them.
// Agent selection and dispatch are separate capabilities.
type ConversationThread struct {
	ID              string   `json:"id"`
	ProjectID       string   `json:"project_id"`
	ChannelID       string   `json:"channel_id"`
	RootMessageID   string   `json:"root_message_id"`
	TaskIDs         []string `json:"task_ids"`
	SelectedAgentID string   `json:"selected_agent_id,omitempty"`
	CreatedAt       int64    `json:"created_at"`
}

type ConversationInvocationInput struct {
	ChannelID string `json:"channel_id"`
	ThreadID  string `json:"thread_id,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	AgentID   string `json:"agent_id"`
	Body      string `json:"body,omitempty"`
	RequestID string `json:"request_id"`
	AuthorID  string `json:"-"`
}

type ConversationInvocation struct {
	Thread  ConversationThread `json:"thread"`
	Message ChannelMessage     `json:"message"`
}
