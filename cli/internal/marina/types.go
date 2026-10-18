package marina

import "encoding/json"

// These types mirror only the parts of Marina's public marina.dev/v1 objects
// that platformctl reads. Raw keeps the complete response for JSON output.

type ObjectMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type Capabilities struct {
	WorkloadTypes []string `json:"workloadTypes"`
	Models        []struct {
		Name  string   `json:"name"`
		Roles []string `json:"roles"`
	} `json:"models"`
	Tools []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tools"`
	Limits struct {
		MaxTimeMinutes int   `json:"maxTimeMinutes"`
		MaxTokens      int64 `json:"maxTokens"`
		MaxAttempts    int   `json:"maxAttempts"`
	} `json:"limits"`
	Raw json.RawMessage `json:"-"`
}

type Budget struct {
	TimeMinutes int   `json:"timeMinutes"`
	MaxTokens   int64 `json:"maxTokens"`
	MaxAttempts int   `json:"maxAttempts"`
}

type ContextItem struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Trust   string `json:"trust"`
	Digest  string `json:"digest"`
	Content string `json:"content,omitempty"`
}

type RunSpec struct {
	Project            string        `json:"project"`
	Workload           string        `json:"workload,omitempty"`
	Title              string        `json:"title"`
	Description        string        `json:"description,omitempty"`
	AcceptanceCriteria string        `json:"acceptanceCriteria"`
	Budget             Budget        `json:"budget"`
	Context            []ContextItem `json:"context,omitempty"`
}

type Usage struct {
	Tokens      int64 `json:"tokens"`
	Attempts    int   `json:"attempts"`
	MaxAttempts int   `json:"maxAttempts"`
}

type Evidence struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Passed         bool   `json:"passed"`
	Summary        string `json:"summary"`
	SourceRevision string `json:"sourceRevision"`
	ArtifactDigest string `json:"artifactDigest"`
}

type Run struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     RunSpec    `json:"spec"`
	Status   struct {
		Phase   string `json:"phase"`
		Reason  string `json:"reason"`
		Usage   Usage  `json:"usage"`
		Landing struct {
			Commit string `json:"commit"`
		} `json:"landing"`
		PullRequest struct {
			URL string `json:"url"`
		} `json:"pullRequest"`
		Evidence  []Evidence `json:"evidence"`
		Decisions []string   `json:"decisions"`
	} `json:"status"`
	Raw json.RawMessage `json:"-"`
}

type RunList struct {
	Items []Run           `json:"items"`
	Raw   json.RawMessage `json:"-"`
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
	Budget TurnBudget `json:"budget"`
}

type Turn struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     TurnSpec   `json:"spec"`
	Status   struct {
		Phase  string `json:"phase"`
		Reason string `json:"reason"`
		Reply  struct {
			Answer string `json:"answer"`
		} `json:"reply"`
		Usage     Usage    `json:"usage"`
		Decisions []string `json:"decisions"`
	} `json:"status"`
	Raw json.RawMessage `json:"-"`
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
		Response       struct {
			Answer string `json:"answer"`
		} `json:"response"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
	Raw json.RawMessage `json:"-"`
}

type DecisionList struct {
	Items []Decision      `json:"items"`
	Raw   json.RawMessage `json:"-"`
}

type Event struct {
	Sequence int64          `json:"sequence"`
	Type     string         `json:"type"`
	Time     string         `json:"time"`
	Message  string         `json:"message,omitempty"`
	Fields   map[string]any `json:"fields,omitempty"`
}

type EventPage struct {
	Items     []Event         `json:"items"`
	NextAfter int64           `json:"nextAfter"`
	Raw       json.RawMessage `json:"-"`
}
