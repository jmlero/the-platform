package appdeck

type Health struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Time    string `json:"time"`
}

type Bootstrap struct {
	Tasks     []Task     `json:"tasks"`
	Agents    []Agent    `json:"agents"`
	Decisions []Decision `json:"decisions"`
	Events    []Event    `json:"events"`
	Now       int64      `json:"now"`
}

type Task struct {
	ID                 string  `json:"id"`
	ProjectID          string  `json:"project_id"`
	Title              string  `json:"title"`
	Description        string  `json:"description"`
	AcceptanceCriteria string  `json:"acceptance_criteria"`
	Status             string  `json:"status"`
	Priority           string  `json:"priority"`
	BudgetMinutes      int     `json:"budget_minutes"`
	AssignedAgentID    *string `json:"assigned_agent_id,omitempty"`
	Version            int     `json:"version"`
	CreatedAt          int64   `json:"created_at"`
	UpdatedAt          int64   `json:"updated_at"`
}

type CreateTaskInput struct {
	ProjectID          string `json:"project_id,omitempty"`
	Title              string `json:"title"`
	Description        string `json:"description,omitempty"`
	AcceptanceCriteria string `json:"acceptance_criteria,omitempty"`
	Status             string `json:"status,omitempty"`
	Priority           string `json:"priority,omitempty"`
	BudgetMinutes      int    `json:"budget_minutes,omitempty"`
}

type CreateCommandInput struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

type Command struct {
	ID          string `json:"id"`
	RunID       string `json:"run_id"`
	Sequence    int64  `json:"sequence"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	RequestedBy string `json:"requested_by"`
	Result      string `json:"result,omitempty"`
	RunVersion  int64  `json:"run_version"`
	FenceToken  int64  `json:"fence_token"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type TaskDetail struct {
	Task      Task       `json:"task"`
	Runs      []Run      `json:"runs"`
	Events    []Event    `json:"events"`
	Decisions []Decision `json:"decisions"`
	Artifacts []Artifact `json:"artifacts"`
}

type Agent struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
	LastSeenAt   int64    `json:"last_seen_at"`
	CreatedAt    int64    `json:"created_at"`
}

type Run struct {
	ID             string `json:"id"`
	TaskID         string `json:"task_id"`
	AgentID        string `json:"agent_id"`
	Status         string `json:"status"`
	Plan           string `json:"plan"`
	ResultSummary  string `json:"result_summary"`
	LeaseExpiresAt *int64 `json:"lease_expires_at,omitempty"`
	StartedAt      int64  `json:"started_at"`
	FinishedAt     *int64 `json:"finished_at,omitempty"`
	UpdatedAt      int64  `json:"updated_at"`
}

type Decision struct {
	ID             string   `json:"id"`
	TaskID         string   `json:"task_id"`
	RunID          *string  `json:"run_id,omitempty"`
	Question       string   `json:"question"`
	Options        []string `json:"options"`
	Recommendation string   `json:"recommendation"`
	Status         string   `json:"status"`
	Answer         string   `json:"answer"`
	CreatedAt      int64    `json:"created_at"`
	AnsweredAt     *int64   `json:"answered_at,omitempty"`
}

type Event struct {
	ID        int64          `json:"id"`
	TaskID    *string        `json:"task_id,omitempty"`
	RunID     *string        `json:"run_id,omitempty"`
	AgentID   *string        `json:"agent_id,omitempty"`
	Type      string         `json:"type"`
	Message   string         `json:"message"`
	Payload   map[string]any `json:"payload"`
	CreatedAt int64          `json:"created_at"`
}

type Artifact struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	RunID     string `json:"run_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	URI       string `json:"uri"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}
