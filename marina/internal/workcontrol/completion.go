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

func WorkerCapabilities() []string {
	return append(MilestoneOneCapabilities(), CapabilityPlanSubmit, CapabilityDecisionInspect,
		CapabilityRunStart, CapabilityRunVerify, CapabilityRunComplete, "evidence.record")
}
