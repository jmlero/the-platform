package platformapi

// Go shapes of the marina.dev/v1 objects in marina/api/openapi.yaml. Tests in
// marina/api check the canonical fixtures against the schemas; these types
// round-trip those fixtures.

type ObjectMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	UID               string            `json:"uid,omitempty"`
	ResourceVersion   string            `json:"resourceVersion,omitempty"`
	Generation        int64             `json:"generation,omitempty"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
	Finalizers        []string          `json:"finalizers,omitempty"`
	DeletionTimestamp string            `json:"deletionTimestamp,omitempty"`
}

type Condition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

type Budget struct {
	TimeMinutes  int    `json:"timeMinutes"`
	MaxTokens    int64  `json:"maxTokens"`
	MaxAttempts  int    `json:"maxAttempts"`
	MaxCostMinor int64  `json:"maxCostMinor,omitempty"`
	Currency     string `json:"currency,omitempty"`
}

type ContextSource struct {
	System   string `json:"system,omitempty"`
	ID       string `json:"id,omitempty"`
	Revision int64  `json:"revision,omitempty"`
}

type ContextItem struct {
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Trust    string         `json:"trust"`
	Digest   string         `json:"digest"`
	Content  string         `json:"content,omitempty"`
	Path     string         `json:"path,omitempty"`
	Revision string         `json:"revision,omitempty"`
	Source   *ContextSource `json:"source,omitempty"`
}

type Usage struct {
	Tokens            int64  `json:"tokens"`
	ReservedTokens    int64  `json:"reservedTokens"`
	CostMinor         *int64 `json:"costMinor,omitempty"`
	ReservedCostMinor *int64 `json:"reservedCostMinor,omitempty"`
	Currency          string `json:"currency,omitempty"`
	Attempts          int    `json:"attempts,omitempty"`
	MaxAttempts       int    `json:"maxAttempts,omitempty"`
}

type Evidence struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Passed         bool   `json:"passed"`
	Summary        string `json:"summary,omitempty"`
	SourceRevision string `json:"sourceRevision"`
	ArtifactDigest string `json:"artifactDigest,omitempty"`
	RecordedAt     string `json:"recordedAt,omitempty"`
}

type TaskProposal struct {
	Title              string `json:"title"`
	Description        string `json:"description,omitempty"`
	AcceptanceCriteria string `json:"acceptanceCriteria"`
}

type SecretKey struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type Repository struct {
	Provider         string    `json:"provider"`
	URL              string    `json:"url"`
	BaseBranch       string    `json:"baseBranch"`
	CredentialSecret SecretKey `json:"credentialSecret"`
}

type ProjectSpec struct {
	DisplayName     string     `json:"displayName,omitempty"`
	Repository      Repository `json:"repository"`
	DefaultWorkload string     `json:"defaultWorkload,omitempty"`
}

type ProjectStatus struct {
	ObservedGeneration int64       `json:"observedGeneration,omitempty"`
	BindingVersion     int64       `json:"bindingVersion,omitempty"`
	Conditions         []Condition `json:"conditions,omitempty"`
}

type Project struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   ObjectMeta     `json:"metadata"`
	Spec       ProjectSpec    `json:"spec"`
	Status     *ProjectStatus `json:"status,omitempty"`
}

type Limits struct {
	MaxTimeMinutes int   `json:"maxTimeMinutes,omitempty"`
	MaxTokens      int64 `json:"maxTokens,omitempty"`
	MaxAttempts    int   `json:"maxAttempts,omitempty"`
}

type Check struct {
	ID        string   `json:"id"`
	Directory string   `json:"directory,omitempty"`
	Command   []string `json:"command"`
}

type RepositoryChange struct {
	Model        string   `json:"model"`
	Checks       []Check  `json:"checks"`
	AllowedPaths []string `json:"allowedPaths"`
	PlanApproval string   `json:"planApproval,omitempty"`
	Review       *struct {
		Enabled bool   `json:"enabled"`
		Model   string `json:"model,omitempty"`
	} `json:"review,omitempty"`
	Delivery struct {
		Mode           string   `json:"mode"`
		RequiredChecks []string `json:"requiredChecks,omitempty"`
	} `json:"delivery"`
	Repair *struct {
		MaxFailures int `json:"maxFailures"`
	} `json:"repair,omitempty"`
	Limits *Limits `json:"limits,omitempty"`
}

type ConversationChoices struct {
	Model           string   `json:"model"`
	RepositoryTools *bool    `json:"repositoryTools,omitempty"`
	MCPTools        []string `json:"mcpTools,omitempty"`
	Proposals       *struct {
		Task     bool `json:"task,omitempty"`
		PRD      bool `json:"prd,omitempty"`
		PRDTasks bool `json:"prdTasks,omitempty"`
	} `json:"proposals,omitempty"`
	Limits *Limits `json:"limits,omitempty"`
}

type WorkloadSpec struct {
	Type             string               `json:"type"`
	RepositoryChange *RepositoryChange    `json:"repositoryChange,omitempty"`
	Conversation     *ConversationChoices `json:"conversation,omitempty"`
}

type WorkloadStatus struct {
	ObservedGeneration int64       `json:"observedGeneration,omitempty"`
	Digest             string      `json:"digest,omitempty"`
	Conditions         []Condition `json:"conditions,omitempty"`
}

type Workload struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   ObjectMeta      `json:"metadata"`
	Spec       WorkloadSpec    `json:"spec"`
	Status     *WorkloadStatus `json:"status,omitempty"`
}

type Publish struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Digest  string `json:"digest"`
}

type RunSpec struct {
	Project            string        `json:"project"`
	Workload           string        `json:"workload,omitempty"`
	Title              string        `json:"title"`
	Description        string        `json:"description,omitempty"`
	AcceptanceCriteria string        `json:"acceptanceCriteria"`
	Budget             Budget        `json:"budget"`
	Context            []ContextItem `json:"context,omitempty"`
	Publish            *Publish      `json:"publish,omitempty"`
	DependsOn          []string      `json:"dependsOn,omitempty"`
	Cancel             bool          `json:"cancel,omitempty"`
}

type Revision struct {
	Base   string `json:"base,omitempty"`
	Head   string `json:"head,omitempty"`
	Branch string `json:"branch,omitempty"`
}

type Landing struct {
	Commit     string `json:"commit"`
	ObservedAt string `json:"observedAt"`
}

type PullRequest struct {
	URL    string `json:"url,omitempty"`
	Number int64  `json:"number,omitempty"`
}

type RunStatus struct {
	ObservedGeneration int64        `json:"observedGeneration,omitempty"`
	Phase              string       `json:"phase,omitempty"`
	Reason             string       `json:"reason,omitempty"`
	Conditions         []Condition  `json:"conditions,omitempty"`
	Usage              *Usage       `json:"usage,omitempty"`
	Revision           *Revision    `json:"revision,omitempty"`
	Landing            *Landing     `json:"landing,omitempty"`
	PullRequest        *PullRequest `json:"pullRequest,omitempty"`
	Evidence           []Evidence   `json:"evidence,omitempty"`
	Decisions          []string     `json:"decisions,omitempty"`
	LastEventSequence  int64        `json:"lastEventSequence,omitempty"`
	StartedAt          string       `json:"startedAt,omitempty"`
	FinishedAt         string       `json:"finishedAt,omitempty"`
}

type Run struct {
	APIVersion string     `json:"apiVersion"`
	Kind       string     `json:"kind"`
	Metadata   ObjectMeta `json:"metadata"`
	Spec       RunSpec    `json:"spec"`
	Status     *RunStatus `json:"status,omitempty"`
}

type TurnMessage struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Body string `json:"body"`
}

type TurnBudget struct {
	TimeMinutes int   `json:"timeMinutes"`
	MaxTokens   int64 `json:"maxTokens"`
}

type TurnSpec struct {
	Project  string `json:"project"`
	Workload string `json:"workload,omitempty"`
	Thread   struct {
		ID       string        `json:"id"`
		Messages []TurnMessage `json:"messages"`
	} `json:"thread"`
	Context []ContextItem `json:"context,omitempty"`
	Budget  TurnBudget    `json:"budget"`
	Cancel  bool          `json:"cancel,omitempty"`
}

type PRDProposal struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Digest  string `json:"digest"`
}

type PRDTasksProposal struct {
	Source string         `json:"source"`
	Tasks  []TaskProposal `json:"tasks"`
}

type Proposals struct {
	Task     *TaskProposal     `json:"task,omitempty"`
	PRD      *PRDProposal      `json:"prd,omitempty"`
	PRDTasks *PRDTasksProposal `json:"prdTasks,omitempty"`
}

type TurnStatus struct {
	ObservedGeneration int64       `json:"observedGeneration,omitempty"`
	Phase              string      `json:"phase,omitempty"`
	Reason             string      `json:"reason,omitempty"`
	Conditions         []Condition `json:"conditions,omitempty"`
	RepositoryRevision string      `json:"repositoryRevision,omitempty"`
	Reply              *struct {
		Answer string `json:"answer"`
	} `json:"reply,omitempty"`
	Proposals         *Proposals `json:"proposals,omitempty"`
	Usage             *Usage     `json:"usage,omitempty"`
	Decisions         []string   `json:"decisions,omitempty"`
	LastEventSequence int64      `json:"lastEventSequence,omitempty"`
	StartedAt         string     `json:"startedAt,omitempty"`
	FinishedAt        string     `json:"finishedAt,omitempty"`
}

type Turn struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   ObjectMeta  `json:"metadata"`
	Spec       TurnSpec    `json:"spec"`
	Status     *TurnStatus `json:"status,omitempty"`
}

type DecisionResponse struct {
	Answer     string `json:"answer"`
	AnsweredBy string `json:"answeredBy,omitempty"`
	AnsweredAt string `json:"answeredAt,omitempty"`
}

type DecisionSpec struct {
	Owner struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"owner"`
	Kind           string            `json:"kind"`
	Question       string            `json:"question"`
	Options        []string          `json:"options,omitempty"`
	Recommendation string            `json:"recommendation,omitempty"`
	Details        string            `json:"details,omitempty"`
	ExpiresAt      string            `json:"expiresAt,omitempty"`
	Response       *DecisionResponse `json:"response,omitempty"`
}

type DecisionStatus struct {
	ObservedGeneration int64       `json:"observedGeneration,omitempty"`
	Phase              string      `json:"phase,omitempty"`
	Conditions         []Condition `json:"conditions,omitempty"`
}

type Decision struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   ObjectMeta      `json:"metadata"`
	Spec       DecisionSpec    `json:"spec"`
	Status     *DecisionStatus `json:"status,omitempty"`
}

type Event struct {
	Sequence int64          `json:"sequence"`
	Type     string         `json:"type"`
	Time     string         `json:"time"`
	Message  string         `json:"message,omitempty"`
	Fields   map[string]any `json:"fields,omitempty"`
}

type EventPage struct {
	Items     []Event `json:"items"`
	NextAfter int64   `json:"nextAfter,omitempty"`
}

type ModelCapability struct {
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

type ToolCapability struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Capabilities struct {
	APIVersion    string            `json:"apiVersion"`
	Kind          string            `json:"kind"`
	WorkloadTypes []string          `json:"workloadTypes"`
	Models        []ModelCapability `json:"models"`
	Tools         []ToolCapability  `json:"tools"`
	Limits        Limits            `json:"limits"`
}

type DecisionAnswer struct {
	Answer string `json:"answer"`
}
