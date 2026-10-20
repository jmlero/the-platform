package marina

// These types mirror only the parts of Marina's marina.dev/v1 objects that
// Deck sends or reads. The contract is marina/api/openapi.yaml.

type ObjectMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type Condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type Limits struct {
	MaxTimeMinutes int   `json:"maxTimeMinutes,omitempty"`
	MaxTokens      int64 `json:"maxTokens,omitempty"`
	MaxAttempts    int   `json:"maxAttempts,omitempty"`
}

type Capabilities struct {
	WorkloadTypes []string `json:"workloadTypes"`
	Limits        Limits   `json:"limits"`
}

type Project struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		DisplayName string `json:"displayName"`
		Repository  struct {
			Provider   string `json:"provider"`
			URL        string `json:"url"`
			BaseBranch string `json:"baseBranch"`
		} `json:"repository"`
		DefaultWorkload string `json:"defaultWorkload"`
	} `json:"spec"`
	Status struct {
		BindingVersion int64       `json:"bindingVersion"`
		Conditions     []Condition `json:"conditions"`
	} `json:"status"`
}

// Ready reports whether Marina has bound the Project's repository.
func (p Project) Ready() bool {
	for _, condition := range p.Status.Conditions {
		if condition.Type == "Ready" {
			return condition.Status == "True"
		}
	}
	return false
}

type Workload struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Type             string `json:"type"`
		RepositoryChange *struct {
			PlanApproval string `json:"planApproval"`
			Delivery     struct {
				Mode string `json:"mode"`
			} `json:"delivery"`
			Limits *Limits `json:"limits"`
		} `json:"repositoryChange"`
		Conversation *struct {
			Limits *Limits `json:"limits"`
		} `json:"conversation"`
	} `json:"spec"`
	Status struct {
		Digest     string      `json:"digest"`
		Conditions []Condition `json:"conditions"`
	} `json:"status"`
}

type Budget struct {
	TimeMinutes  int    `json:"timeMinutes"`
	MaxTokens    int64  `json:"maxTokens"`
	MaxAttempts  int    `json:"maxAttempts"`
	MaxCostMinor int64  `json:"maxCostMinor,omitempty"`
	Currency     string `json:"currency,omitempty"`
}

type ContextSource struct {
	System   string `json:"system"`
	ID       string `json:"id"`
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
}

type Evidence struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Passed         bool   `json:"passed"`
	Summary        string `json:"summary"`
	SourceRevision string `json:"sourceRevision"`
	ArtifactDigest string `json:"artifactDigest"`
	RecordedAt     string `json:"recordedAt"`
}

// Usage is Marina's model usage for one Run; cost is in minor units.
type Usage struct {
	Tokens            int64  `json:"tokens"`
	ReservedTokens    int64  `json:"reservedTokens"`
	CostMinor         int64  `json:"costMinor"`
	ReservedCostMinor int64  `json:"reservedCostMinor"`
	Currency          string `json:"currency"`
}

type Run struct {
	Metadata ObjectMeta `json:"metadata"`
	Status   struct {
		Phase      string      `json:"phase"`
		Reason     string      `json:"reason"`
		Conditions []Condition `json:"conditions"`
		Usage      *Usage      `json:"usage"`
		Landing    *struct {
			Commit     string `json:"commit"`
			ObservedAt string `json:"observedAt"`
		} `json:"landing"`
		PullRequest *struct {
			URL string `json:"url"`
		} `json:"pullRequest"`
		Evidence          []Evidence `json:"evidence"`
		Decisions         []string   `json:"decisions"`
		LastEventSequence int64      `json:"lastEventSequence"`
	} `json:"status"`
}

type Event struct {
	Sequence int64          `json:"sequence"`
	Type     string         `json:"type"`
	Time     string         `json:"time"`
	Message  string         `json:"message"`
	Fields   map[string]any `json:"fields"`
}

type EventPage struct {
	Items     []Event `json:"items"`
	NextAfter int64   `json:"nextAfter"`
}

type Decision struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Owner struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"owner"`
		Kind           string   `json:"kind"`
		Question       string   `json:"question"`
		Options        []string `json:"options"`
		Recommendation string   `json:"recommendation"`
		Details        string   `json:"details"`
		ExpiresAt      string   `json:"expiresAt"`
		Response       *struct {
			Answer     string `json:"answer"`
			AnsweredBy string `json:"answeredBy"`
		} `json:"response"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
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

type TurnThread struct {
	ID       string        `json:"id"`
	Messages []TurnMessage `json:"messages"`
}

type TurnSpec struct {
	Project  string        `json:"project"`
	Workload string        `json:"workload,omitempty"`
	Thread   TurnThread    `json:"thread"`
	Context  []ContextItem `json:"context,omitempty"`
	Budget   TurnBudget    `json:"budget"`
}

type TaskProposal struct {
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptanceCriteria"`
}

type Turn struct {
	Metadata ObjectMeta `json:"metadata"`
	Status   struct {
		Phase  string `json:"phase"`
		Reason string `json:"reason"`
		Reply  *struct {
			Answer string `json:"answer"`
		} `json:"reply"`
		Proposals *struct {
			Task *TaskProposal `json:"task"`
			PRD  *struct {
				Path    string `json:"path"`
				Title   string `json:"title"`
				Content string `json:"content"`
			} `json:"prd"`
			PRDTasks *struct {
				Source string         `json:"source"`
				Tasks  []TaskProposal `json:"tasks"`
			} `json:"prdTasks"`
		} `json:"proposals"`
		Decisions []string `json:"decisions"`
	} `json:"status"`
}
