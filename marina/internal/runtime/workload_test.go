package runtime

import (
	"strings"
	"testing"
	"time"
)

func TestAnalysisSpecificationHasNoRepositoryAndBindsInputsAndProfile(t *testing.T) {
	now := time.Now()
	s := Specification{ExecutionID: "e", AttemptID: "a", IdempotencyKey: "k", Workload: "diagnostic-analysis/v1", InputDigest: strings.Repeat("a", 64), ProfileDigest: strings.Repeat("b", 64), WorkerImage: "worker@sha256:" + strings.Repeat("c", 64), ContextURI: "journal://e", ContextDigest: "context", HarnessVersion: "langchain-create-agent/v1", Limits: Limits{1, 1, 1, 1, 1}, NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral-read-only-root", AuthorityFence: 1, DeadlineAt: now.Add(time.Hour), PolicyVersion: "v1"}
	if err := s.Validate(now); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Specification){func(s *Specification) { s.InputDigest = "" }, func(s *Specification) { s.ProfileDigest = "" }, func(s *Specification) { s.Workload = "unknown" }, func(s *Specification) { s.RepositoryURI = "https://example.com/repo" }, func(s *Specification) { s.SourceRevision = "fake-commit" }} {
		copy := s
		mutate(&copy)
		if copy.Validate(now) == nil {
			t.Fatal("invalid analysis specification accepted")
		}
	}
}
