package flow

import (
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workresult"
	"testing"
)

func TestPRDPublicationRequiresExactApprovedFileAndRepository(t *testing.T) {
	repo := workcontrol.RepositoryBinding{Version: 1, Repository: "owner/repo", CloneURL: "https://github.com/owner/repo.git", BaseBranch: "main"}
	doc := workcontrol.PRDDocument{PRDDraft: workcontrol.PRDDraft{Title: "Filters", Path: "docs/prd/filters.md", Content: "# Filters\n"}, MessageID: "msg", Repository: repo, ApprovalDigest: "approval", ApprovedBy: "operator:test", ApprovedAt: 1}
	doc.Digest = doc.ContentDigest()
	task := workcontrol.WorkItem{PRD: &doc, Context: &workcontrol.TaskContext{Repository: &repo}}
	candidate := workresult.Candidate{Changes: []workresult.Change{{Path: doc.Path, Content: []byte(doc.Content)}}}
	if err := validatePRDCandidate(task, candidate); err != nil {
		t.Fatal(err)
	}
	candidate.Changes[0].Content = []byte("rewritten")
	if validatePRDCandidate(task, candidate) == nil {
		t.Fatal("rewritten approved text passed")
	}
	candidate.Changes[0].Content = []byte(doc.Content)
	candidate.Changes = append(candidate.Changes, workresult.Change{Path: "src/main.go", Content: []byte("extra")})
	if validatePRDCandidate(task, candidate) == nil {
		t.Fatal("unrelated change passed")
	}
	candidate.Changes = candidate.Changes[:1]
	candidate.Changes[0].Delete = true
	if validatePRDCandidate(task, candidate) == nil {
		t.Fatal("deletion passed")
	}
	candidate.Changes[0].Delete = false
	repo.Version = 2
	if validatePRDCandidate(task, candidate) == nil {
		t.Fatal("changed repository passed")
	}
}
