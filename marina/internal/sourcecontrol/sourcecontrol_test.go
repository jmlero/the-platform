package sourcecontrol

import (
	"context"
	"testing"
)

type contractStub struct{}

func (contractStub) CreateBranch(context.Context, BranchRequest) (Revision, error) {
	return Revision{Commit: "abc123", Ref: "refs/heads/marina/task"}, nil
}
func (contractStub) WriteChanges(context.Context, WriteRequest) (Revision, error) {
	return Revision{Commit: "def456", Ref: "refs/heads/marina/task"}, nil
}
func (contractStub) CreatePullRequest(context.Context, PullRequestRequest) (PullRequest, error) {
	return PullRequest{ID: "pr-1", HeadRevision: "def456"}, nil
}
func (contractStub) CurrentHead(context.Context, HeadRequest) (Revision, error) {
	return Revision{Commit: "def456", Ref: "refs/heads/marina/task"}, nil
}
func (contractStub) ObserveReviews(context.Context, ObservationRequest) ([]ReviewObservation, error) {
	return []ReviewObservation{{ID: "review-1", HeadRevision: "def456", State: "approved"}}, nil
}
func (contractStub) ObserveChecks(context.Context, ObservationRequest) ([]CheckObservation, error) {
	return []CheckObservation{{ID: "check-1", HeadRevision: "def456", State: "passed"}}, nil
}

func TestPortKeepsGuardedWritesAndProviderObservationsNarrow(t *testing.T) {
	var provider Port = contractStub{}
	request := WriteRequest{
		Repository: "repository-1", Branch: "refs/heads/marina/task", ExpectedHead: "abc123",
		IdempotencyKey: "write-task-1", AuthorityFence: 7,
		Changes: []Change{{Path: "README.md", ContentDigest: "sha256:readme", Operation: ChangeUpdate}},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("valid guarded write: %v", err)
	}
	revision, err := provider.WriteChanges(context.Background(), request)
	if err != nil || revision.Commit != "def456" {
		t.Fatalf("write result = %#v, err %v", revision, err)
	}
	request.ExpectedHead = ""
	if err := request.Validate(); err == nil {
		t.Fatal("unguarded write was accepted")
	}
	request.ExpectedHead = "abc123"
	request.Changes[0].Path = "../outside"
	if err := request.Validate(); err == nil {
		t.Fatal("path traversal was accepted")
	}
}
