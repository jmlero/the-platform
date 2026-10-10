package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/conversationbyo"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
)

type contextSource struct {
	testSource
	selected workcontrol.ResolvedContext
}

func (s *contextSource) GetConversationContext(context.Context, string) (workcontrol.ResolvedContext, error) {
	return s.selected, nil
}

type readRepository struct {
	head      string
	reads     []string
	headCalls int
	content   []byte
}

func (r *readRepository) CurrentHead(context.Context, sourcecontrol.HeadRequest) (sourcecontrol.Revision, error) {
	r.headCalls++
	return sourcecontrol.Revision{Commit: r.head, Ref: "main"}, nil
}
func (r *readRepository) ListRepositoryFiles(context.Context, string, string) ([]sourcecontrol.RepositoryFile, error) {
	return []sourcecontrol.RepositoryFile{{Path: "main.go", Size: 12}}, nil
}
func (r *readRepository) ReadReviewFile(_ context.Context, repository, revision, filename string) ([]byte, bool, error) {
	r.reads = append(r.reads, repository+"@"+revision+"/"+filename)
	if r.content != nil {
		return r.content, true, nil
	}
	return []byte("package main"), true, nil
}

func TestConversationFreezesRepositoryAndUsesSavedSelectionAcrossRecovery(t *testing.T) {
	ctx := context.Background()
	journalPath := t.TempDir() + "/journal.db"
	journal, err := sqlite.Open(ctx, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	blobs, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	message := workcontrol.ConversationMessage{ID: "msg_context", ProjectID: "prj_1", ChannelID: "chn_1", ThreadID: "thr_1", AuthorType: "human", Body: "Explain main.go", InvokedAgentID: "agt_1", InvocationRequestID: "req_context"}
	turn, err := journal.EnsureConversationTurn(ctx, message, time.Now().Add(time.Minute), 20000)
	if err != nil {
		t.Fatal(err)
	}
	binding := workcontrol.RepositoryBinding{Repository: "owner/repo", CloneURL: "https://github.com/owner/repo.git", BaseBranch: "main", Version: 3}
	source := &contextSource{testSource: testSource{message: message}, selected: workcontrol.ResolvedContext{Selection: &workcontrol.TaskContext{Repository: &binding, Sources: []workcontrol.ContextReference{}, Messages: []workcontrol.ContextReference{}}}}
	repo := &readRepository{head: strings.Repeat("a", 40)}
	c := Controller{ProjectID: "prj_1", AgentID: "agt_1", PolicyVersion: "test", Model: workerapi.Model{Name: "fixture", MaxOutputTokens: 100}, Profile: Profile{BudgetMinutes: 1, MaxTokens: 20000, Engine: rt.EngineSelection{Backend: "kagent", Version: "v", Endpoint: "engine", Namespace: "test", Harness: "conversation"}}, Source: source, Journal: journal, Blobs: blobs, Repository: func(_ context.Context, got workcontrol.RepositoryBinding) (sourcecontrol.RepositoryReader, error) {
		if got != binding {
			return nil, errors.New("binding changed")
		}
		return repo, nil
	}}
	in, err := c.input(ctx, turn)
	if err != nil {
		t.Fatal(err)
	}
	if in.Repository == nil || in.Repository.Revision != repo.head || in.Repository.Binding != binding || in.Repository.TreeDigest == "" {
		t.Fatalf("repository not pinned: %+v", in.Repository)
	}
	encoded, _ := json.Marshal(in)
	digest, err := blobs.Put(encoded)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := c.specification(ctx, turn, "turn-context", strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	if spec.ContextDigest != digest {
		t.Fatal("snapshot changed before dispatch")
	}
	if err := journal.SaveConversationSpecification(ctx, turn.MessageID, spec); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c.Journal = reopened
	turn, err = reopened.GetConversationTurn(ctx, turn.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	repo.head = strings.Repeat("b", 40)
	recovered, err := c.frozenInput(turn)
	if err != nil || recovered.Repository.Revision != strings.Repeat("a", 40) {
		t.Fatalf("recovery refreshed repository: %+v %v", recovered.Repository, err)
	}
	if repo.headCalls != 1 {
		t.Fatalf("saved repository was repinned %d times", repo.headCalls)
	}
	in.Messages[0].Body = "changed"
	if conversationbyo.ValidateInput(in) == nil {
		t.Fatal("changed frozen input accepted")
	}
}
