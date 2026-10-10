package domain

// ConversationOutcomeInput is a service-published, source-bound result of one
// invoked turn. Task intent comes from the operator's invocation; the caller
// cannot change an existing task or broaden an active run.
type ConversationOutcomeInput struct {
	PRDTasks            *PRDTaskProposal `json:"prd_tasks,omitempty"`
	PRD                 *PRDDraft        `json:"prd,omitempty"`
	InvocationMessageID string           `json:"invocation_message_id"`
	AgentID             string           `json:"agent_id"`
	Status              string           `json:"status"`
	Body                string           `json:"body"`
	ResultDigest        string           `json:"result_digest"`
	ProfileID           string           `json:"profile_id,omitempty"`
	ProfileDigest       string           `json:"profile_digest,omitempty"`
	Task                *CreateTaskInput `json:"task,omitempty"`
	FollowupTaskID      string           `json:"followup_task_id,omitempty"`
}

type ConversationOutcome struct {
	InvocationMessageID string `json:"invocation_message_id"`
	MessageID           string `json:"message_id"`
	TaskID              string `json:"task_id,omitempty"`
	FollowupTaskID      string `json:"followup_task_id,omitempty"`
	Status              string `json:"status"`
	ResultDigest        string `json:"result_digest"`
}
