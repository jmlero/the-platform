package workcontrol

import (
	"context"
	"time"
)

type Mutation struct {
	IdempotencyKey  string
	AgentID         string
	ExpectedVersion int64
	FenceToken      int64
}

type EvidenceSubmission struct {
	ID               string
	PlanRevisionID   string
	Type             string
	Producer         string
	Verifier         string
	SubjectDigest    string
	SourceRevision   string
	Command          string
	Outcome          string
	ProvenanceURI    string
	ProvenanceDigest string
	ObservedAt       time.Time
	ExpiresAt        time.Time
	ArtifactURI      string
	ArtifactDigest   string
	PolicyVersion    string
}

type Completion interface {
	StartRun(context.Context, string, Mutation) error
	BeginVerification(context.Context, string, Mutation) error
	RecordEvidence(context.Context, string, Mutation, EvidenceSubmission) (string, error)
	CompleteRun(context.Context, string, Mutation, string, []string) error
}

type LandingObservation struct {
	Repository string `json:"repository"`
	BaseBranch string `json:"base_branch"`
	Commit     string `json:"commit"`
	ObservedAt int64  `json:"observed_at"`
}

type TaskLanding struct {
	LandingObservation
	RunID       string   `json:"run_id"`
	EvidenceIDs []string `json:"evidence_ids"`
}

// LandedCompletion is additive; ordinary v1 completion still requires human acceptance.
type LandedCompletion interface {
	CompleteLandedRun(context.Context, string, Mutation, string, []string, LandingObservation) error
}

func WorkerCapabilities() []string {
	return append(MilestoneOneCapabilities(), CapabilityPlanSubmit, CapabilityDecisionInspect,
		CapabilityRunStart, CapabilityRunVerify, CapabilityRunComplete, "evidence.record", CapabilityContextRead, CapabilityRepositoryRead, CapabilityRepositoryWrite)
}
