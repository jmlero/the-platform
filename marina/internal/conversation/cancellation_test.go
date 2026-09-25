package conversation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
)

type cancellationEngine struct {
	rt.Adapter
	collected bool
}

func (e *cancellationEngine) Start(_ context.Context, spec rt.Specification) (rt.Observation, error) {
	return rt.Observation{Found: true, ProviderHandle: "kagent:" + spec.AttemptID, State: rt.StateRunning}, nil
}
func (e *cancellationEngine) Cancel(_ context.Context, handle string, _ bool) (rt.Observation, error) {
	return rt.Observation{Found: true, ProviderHandle: handle, State: rt.StateCancelled}, nil
}
func (e *cancellationEngine) CollectArtifacts(context.Context, string) ([]rt.Artifact, error) {
	e.collected = true
	return nil, nil
}
func (e *cancellationEngine) Cleanup(context.Context, string) error {
	if !e.collected {
		return rt.ErrConflict
	}
	return nil
}

func TestCancelledConversationCollectsBeforeCleanup(t *testing.T) {
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, t.TempDir()+"/journal.db")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	blobs, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	message := workcontrol.ConversationMessage{ID: "msg_cancel", ProjectID: "prj_1", ChannelID: "chn_1", ThreadID: "thr_1", AuthorType: "human", Body: "Explain", InvokedAgentID: "agt_1", InvocationRequestID: "req_cancel", CreatedAt: time.Now().UnixMilli()}
	model := workerapi.Model{Name: "fixture", MaxOutputTokens: 100, Currency: "USD"}
	engine := &cancellationEngine{}
	profile := Profile{Engine: rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-conversation-byo", Template: "turn-cancel", Revision: strings.Repeat("a", 64)}, MaxTokens: 5000, BudgetMinutes: 5}
	controller := Controller{ProjectID: "prj_1", AgentID: "agt_1", PolicyVersion: "test", Source: testSource{message: message}, Journal: journal, Blobs: blobs, Engine: engine,
		Profile: profile,
		Model:   model, Gateway: NewGateway(journal, []byte(strings.Repeat("k", 32)), model, profile.Digest(model), "test", nil), InstallCredential: func(context.Context, rt.Specification, string) error { return nil }}
	if failures := controller.Cycle(ctx); len(failures) != 0 {
		t.Fatal(failures)
	}
	turn, err := journal.GetConversationTurn(ctx, message.ID)
	if err != nil || turn.Status != "running" {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
	if _, err := journal.ConversationEngineJournal().EnsureEngineAttempt(ctx, turn.Specification); err != nil {
		t.Fatal(err)
	}
	if err := controller.stop(ctx, turn, "Cancelled"); err != nil {
		t.Fatal(err)
	}
	turn, err = journal.GetConversationTurn(ctx, message.ID)
	if err != nil || turn.Status != "blocked" || !engine.collected {
		t.Fatalf("cancelled turn not closed: %+v %v", turn, err)
	}
}
