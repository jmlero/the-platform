package policy

import (
	"testing"
	"time"
)

func TestEvaluatorEnforcesVersionAuthorityAndEveryConsequentialGate(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	policy := Policy{
		ProjectID: "project-1", Version: "policy-v3", ChangeClass: "code", Environment: "development", RiskClass: RiskMedium,
		AllowedActions:   []Action{ActionClaim, ActionRuntimeStart, ActionExternalMutation, ActionCompletion},
		AllowedAuthority: []string{"source.read", "source.write", "pull_request.create"},
		AlwaysHuman:      []Action{ActionExpandAuthority},
		Limits: Limits{MaxAttempts: 3, MaxTokens: 100000, MaxCostMicros: 2_000_000,
			MaxConcurrency: 2, MaxCredentialLifetime: 30 * time.Minute, MaxBlastRadius: BlastRepository},
	}
	firstDigest, err := policy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := policy.Digest()
	if err != nil || firstDigest != secondDigest || firstDigest == "" {
		t.Fatalf("policy digest = %q / %q, err %v", firstDigest, secondDigest, err)
	}
	request := Request{
		ProjectID: "project-1", PolicyVersion: "policy-v3", ChangeClass: "code",
		Environment: "development", RiskClass: RiskMedium, Action: ActionRuntimeStart,
		TaskAuthority: []string{"source.read", "source.write"}, RequestedAuthority: []string{"source.read"},
		Budget: Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 3, MaxTokens: 90000,
			MaxCostMicros: 1_000_000, MaxConcurrency: 1, CredentialExpiresAt: now.Add(20 * time.Minute), BlastRadius: BlastRepository},
	}
	decision := Evaluate(now, policy, request)
	if decision.Verdict != VerdictAllow {
		t.Fatalf("allowed decision = %#v", decision)
	}

	tests := []struct {
		name    string
		mutate  func(*Request)
		verdict Verdict
	}{
		{name: "wrong policy version", mutate: func(value *Request) { value.PolicyVersion = "policy-v2" }, verdict: VerdictDeny},
		{name: "authority exceeds task", mutate: func(value *Request) {
			value.RequestedAuthority = []string{"source.write"}
			value.TaskAuthority = []string{"source.read"}
		}, verdict: VerdictDeny},
		{name: "concurrency exceeds policy", mutate: func(value *Request) { value.Budget.MaxConcurrency = 3 }, verdict: VerdictDeny},
		{name: "credential outlives policy", mutate: func(value *Request) { value.Budget.CredentialExpiresAt = now.Add(31 * time.Minute) }, verdict: VerdictDeny},
		{name: "blast radius exceeds policy", mutate: func(value *Request) { value.Budget.BlastRadius = BlastProduction }, verdict: VerdictDeny},
		{name: "merge denied by default", mutate: func(value *Request) { value.Action = ActionMerge }, verdict: VerdictDeny},
		{name: "release denied by default", mutate: func(value *Request) { value.Action = ActionRelease }, verdict: VerdictDeny},
		{name: "authority expansion needs human", mutate: func(value *Request) { value.Action = ActionExpandAuthority }, verdict: VerdictHumanRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := request
			test.mutate(&changed)
			got := Evaluate(now, policy, changed)
			if got.Verdict != test.verdict || got.Reason == "" {
				t.Fatalf("decision = %#v, want %q", got, test.verdict)
			}
		})
	}
}

func TestBudgetStatusStopsAllExhaustedDimensions(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	budget := Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 3, MaxTokens: 100,
		MaxCostMicros: 1000, MaxConcurrency: 2, CredentialExpiresAt: now.Add(30 * time.Minute), BlastRadius: BlastRepository}
	tests := []struct {
		name  string
		usage Usage
		time  time.Time
	}{
		{name: "time", time: budget.DeadlineAt},
		{name: "attempt", time: now, usage: Usage{Attempts: 3}},
		{name: "tokens", time: now, usage: Usage{Tokens: 100}},
		{name: "cost", time: now, usage: Usage{CostMicros: 1000}},
		{name: "concurrency", time: now, usage: Usage{Concurrency: 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status := CheckBudget(test.time, budget, test.usage)
			if !status.Exhausted || status.Reason == "" {
				t.Fatalf("budget status = %#v", status)
			}
		})
	}
}
