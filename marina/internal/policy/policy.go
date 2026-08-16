package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

type Action string

const (
	ActionClaim            Action = "claim"
	ActionRuntimeStart     Action = "runtime_start"
	ActionExternalMutation Action = "external_mutation"
	ActionCompletion       Action = "completion"
	ActionExpandAuthority  Action = "expand_authority"
	ActionMerge            Action = "merge"
	ActionRelease          Action = "release"
)

type RiskClass string

const (
	RiskLow      RiskClass = "low"
	RiskMedium   RiskClass = "medium"
	RiskHigh     RiskClass = "high"
	RiskCritical RiskClass = "critical"
)

type BlastRadius string

const (
	BlastNone       BlastRadius = "none"
	BlastRepository BlastRadius = "repository"
	BlastNonProd    BlastRadius = "non_production"
	BlastProduction BlastRadius = "production"
)

type Limits struct {
	MaxAttempts           int           `json:"max_attempts"`
	MaxTokens             int64         `json:"max_tokens"`
	MaxCostMicros         int64         `json:"max_cost_micros"`
	MaxConcurrency        int           `json:"max_concurrency"`
	MaxCredentialLifetime time.Duration `json:"max_credential_lifetime"`
	MaxBlastRadius        BlastRadius   `json:"max_blast_radius"`
}

type Policy struct {
	ProjectID        string    `json:"project_id"`
	Version          string    `json:"version"`
	ChangeClass      string    `json:"change_class"`
	Environment      string    `json:"environment"`
	RiskClass        RiskClass `json:"risk_class"`
	AllowedActions   []Action  `json:"allowed_actions"`
	AllowedAuthority []string  `json:"allowed_authority"`
	AlwaysHuman      []Action  `json:"always_human"`
	Limits           Limits    `json:"limits"`
}

type Budget struct {
	DeadlineAt          time.Time
	MaxAttempts         int
	MaxTokens           int64
	MaxCostMicros       int64
	MaxConcurrency      int
	CredentialExpiresAt time.Time
	BlastRadius         BlastRadius
}

type Usage struct {
	Attempts    int
	Tokens      int64
	CostMicros  int64
	Concurrency int
}

type Request struct {
	ProjectID          string
	PolicyVersion      string
	ChangeClass        string
	Environment        string
	RiskClass          RiskClass
	Action             Action
	TaskAuthority      []string
	RequestedAuthority []string
	Budget             Budget
}

type Verdict string

const (
	VerdictAllow         Verdict = "allow"
	VerdictDeny          Verdict = "deny"
	VerdictHumanRequired Verdict = "human_required"
)

type Decision struct {
	Verdict       Verdict
	PolicyVersion string
	Reason        string
}

func (policy Policy) Validate() error {
	if strings.TrimSpace(policy.ProjectID) == "" || strings.TrimSpace(policy.Version) == "" ||
		strings.TrimSpace(policy.ChangeClass) == "" || strings.TrimSpace(policy.Environment) == "" ||
		!validRisk(policy.RiskClass) || len(policy.AllowedActions) == 0 || policy.Limits.MaxAttempts < 1 ||
		policy.Limits.MaxConcurrency < 1 || policy.Limits.MaxCredentialLifetime <= 0 ||
		blastRank(policy.Limits.MaxBlastRadius) < 0 {
		return fmt.Errorf("versioned policy scope and bounded limits are required")
	}
	for _, action := range append(slices.Clone(policy.AllowedActions), policy.AlwaysHuman...) {
		if !validAction(action) {
			return fmt.Errorf("policy contains unknown action %q", action)
		}
	}
	return nil
}

func (policy Policy) Digest() (string, error) {
	canonical := policy
	canonical.AllowedActions = slices.Clone(policy.AllowedActions)
	canonical.AllowedAuthority = slices.Clone(policy.AllowedAuthority)
	canonical.AlwaysHuman = slices.Clone(policy.AlwaysHuman)
	slices.Sort(canonical.AllowedActions)
	slices.Sort(canonical.AllowedAuthority)
	slices.Sort(canonical.AlwaysHuman)
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode policy: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func Evaluate(now time.Time, policy Policy, request Request) Decision {
	decision := Decision{Verdict: VerdictDeny, PolicyVersion: policy.Version}
	if err := policy.Validate(); err != nil {
		decision.Reason = "the selected policy snapshot is invalid"
		return decision
	}
	if strings.TrimSpace(policy.ProjectID) == "" || strings.TrimSpace(policy.Version) == "" ||
		policy.ProjectID != request.ProjectID || policy.Version != request.PolicyVersion ||
		policy.ChangeClass != request.ChangeClass || policy.Environment != request.Environment ||
		policy.RiskClass != request.RiskClass {
		decision.Reason = "request does not match the exact versioned project policy scope"
		return decision
	}
	if slices.Contains(policy.AlwaysHuman, request.Action) {
		decision.Verdict = VerdictHumanRequired
		decision.Reason = "the versioned policy reserves this action for a human decision"
		return decision
	}
	if !slices.Contains(policy.AllowedActions, request.Action) {
		decision.Reason = "the requested action is not allowed by policy"
		return decision
	}
	if !subset(request.RequestedAuthority, request.TaskAuthority) {
		decision.Reason = "requested authority exceeds the frozen task authority"
		return decision
	}
	if !subset(request.RequestedAuthority, policy.AllowedAuthority) {
		decision.Reason = "requested authority exceeds the versioned policy"
		return decision
	}
	if reason := validateBudget(now, policy.Limits, request.Budget); reason != "" {
		decision.Reason = reason
		return decision
	}
	decision.Verdict = VerdictAllow
	decision.Reason = "the exact policy version authorizes the bounded action"
	return decision
}

func validRisk(value RiskClass) bool {
	switch value {
	case RiskLow, RiskMedium, RiskHigh, RiskCritical:
		return true
	default:
		return false
	}
}

func validAction(value Action) bool {
	switch value {
	case ActionClaim, ActionRuntimeStart, ActionExternalMutation, ActionCompletion,
		ActionExpandAuthority, ActionMerge, ActionRelease:
		return true
	default:
		return false
	}
}

func validateBudget(now time.Time, limits Limits, budget Budget) string {
	if budget.DeadlineAt.IsZero() || !budget.DeadlineAt.After(now) || budget.MaxAttempts < 1 ||
		budget.MaxTokens < 0 || budget.MaxCostMicros < 0 || budget.MaxConcurrency < 1 ||
		budget.CredentialExpiresAt.IsZero() || !budget.CredentialExpiresAt.After(now) ||
		budget.CredentialExpiresAt.After(budget.DeadlineAt) {
		return "the requested budget is invalid or already expired"
	}
	if limits.MaxAttempts < 1 || limits.MaxConcurrency < 1 || limits.MaxCredentialLifetime <= 0 {
		return "the policy has no valid bounded limits"
	}
	if budget.MaxAttempts > limits.MaxAttempts ||
		(limits.MaxTokens > 0 && budget.MaxTokens > limits.MaxTokens) ||
		(limits.MaxCostMicros > 0 && budget.MaxCostMicros > limits.MaxCostMicros) ||
		budget.MaxConcurrency > limits.MaxConcurrency {
		return "the requested budget exceeds the versioned policy ceilings"
	}
	if budget.CredentialExpiresAt.After(now.Add(limits.MaxCredentialLifetime)) {
		return "credential lifetime exceeds the versioned policy"
	}
	if blastRank(budget.BlastRadius) < 0 || blastRank(budget.BlastRadius) > blastRank(limits.MaxBlastRadius) {
		return "blast radius exceeds the versioned policy"
	}
	return ""
}

type BudgetStatus struct {
	Exhausted bool
	Reason    string
}

func CheckBudget(now time.Time, budget Budget, usage Usage) BudgetStatus {
	switch {
	case !now.Before(budget.DeadlineAt):
		return BudgetStatus{Exhausted: true, Reason: "time budget exhausted"}
	case usage.Attempts >= budget.MaxAttempts:
		return BudgetStatus{Exhausted: true, Reason: "attempt budget exhausted"}
	case budget.MaxTokens > 0 && usage.Tokens >= budget.MaxTokens:
		return BudgetStatus{Exhausted: true, Reason: "token budget exhausted"}
	case budget.MaxCostMicros > 0 && usage.CostMicros >= budget.MaxCostMicros:
		return BudgetStatus{Exhausted: true, Reason: "cost budget exhausted"}
	case usage.Concurrency >= budget.MaxConcurrency:
		return BudgetStatus{Exhausted: true, Reason: "concurrency budget exhausted"}
	default:
		return BudgetStatus{}
	}
}

func subset(values, allowed []string) bool {
	set := make(map[string]struct{}, len(allowed))
	for _, item := range allowed {
		set[strings.TrimSpace(item)] = struct{}{}
	}
	for _, item := range values {
		if _, ok := set[strings.TrimSpace(item)]; !ok {
			return false
		}
	}
	return true
}

func blastRank(value BlastRadius) int {
	switch value {
	case BlastNone:
		return 0
	case BlastRepository:
		return 1
	case BlastNonProd:
		return 2
	case BlastProduction:
		return 3
	default:
		return -1
	}
}
