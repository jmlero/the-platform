package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// fakeTemplates prepares every template after a configured number of
// pending reads and records which attempt resources were removed.
type fakeTemplates struct {
	pending int
	ensured map[string]rt.EngineTemplate
	deleted []string
}

func (f *fakeTemplates) EnsureEngineTemplate(_ context.Context, t rt.EngineTemplate) (string, error) {
	if f.ensured == nil {
		f.ensured = map[string]rt.EngineTemplate{}
	}
	f.ensured[t.Name] = t
	if f.pending > 0 {
		f.pending--
		return "", rt.ErrEngineTemplatePending
	}
	return fakeRevision(t.Name), nil
}

func (f *fakeTemplates) DeleteEngineTemplate(_ context.Context, namespace, name string) error {
	f.deleted = append(f.deleted, namespace+"/"+name)
	return nil
}

func fakeRevision(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

func testProfile() Profile {
	return Profile{Engine: rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083",
		Namespace: "kint-engine", Harness: "kint-conversation-byo"}, MaxTokens: 5000, BudgetMinutes: 5}
}

type turnsSource struct {
	testSource
	messages []workcontrol.ConversationMessage
}

func (s turnsSource) ListConversationInvocations(_ context.Context, _ string, afterID int64) ([]workcontrol.ConversationInvocation, error) {
	var items []workcontrol.ConversationInvocation
	for i, m := range s.messages {
		if int64(i+1) > afterID {
			items = append(items, workcontrol.ConversationInvocation{EventID: int64(i + 1), InvokedAt: m.CreatedAt, Message: m})
		}
	}
	return items, nil
}

func (s turnsSource) PageConversationMessages(context.Context, string, string) (workcontrol.ConversationMessagePage, error) {
	return workcontrol.ConversationMessagePage{Items: s.messages}, nil
}

func TestEveryConversationTurnRunsOnItsOwnPreparedTemplate(t *testing.T) {
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
	now := time.Now().UnixMilli()
	first := workcontrol.ConversationMessage{ID: "msg_first", ProjectID: "prj_1", ChannelID: "chn_1", ThreadID: "thr_1", AuthorType: "human", Body: "First", InvokedAgentID: "agt_1", InvocationRequestID: "req_first", CreatedAt: now}
	second := first
	second.ID, second.Body, second.InvocationRequestID = "msg_second", "Second", "req_second"
	model := workerapi.Model{Name: "fixture", MaxOutputTokens: 100, Currency: "USD"}
	profile := testProfile()
	templates := &fakeTemplates{pending: 1}
	engine := &testEngine{blobs: blobs}
	c := Controller{ProjectID: "prj_1", AgentID: "agt_1", PolicyVersion: "test", Source: turnsSource{messages: []workcontrol.ConversationMessage{first, second}}, Journal: journal, Blobs: blobs,
		Engine: engine, Templates: templates, Profile: profile, Model: model, Gateway: NewGateway(journal, []byte(strings.Repeat("k", 32)), model, profile.Digest(model), "test", nil),
		InstallCredential: func(context.Context, rt.Specification, string) error { return nil }}

	// The template is not prepared yet: nothing is frozen or started.
	if failures := c.Cycle(ctx); len(failures) != 0 {
		t.Fatal(failures)
	}
	turn, err := journal.GetConversationTurn(ctx, first.ID)
	if err != nil || turn.SpecificationDigest != "" || engine.starts != 0 {
		t.Fatalf("started before the template was prepared: %+v starts=%d err=%v", turn, engine.starts, err)
	}
	for range 3 {
		if failures := c.Cycle(ctx); len(failures) != 0 {
			t.Fatal(failures)
		}
	}
	names := map[string]bool{}
	for _, message := range []workcontrol.ConversationMessage{first, second} {
		turn, err := journal.GetConversationTurn(ctx, message.ID)
		if err != nil || turn.Status != "completed" || turn.PublishedMessageID == "" {
			t.Fatalf("turn %s: %+v %v", message.ID, turn, err)
		}
		name := rt.EngineTemplateName("conversation-" + message.ID)
		selection := turn.Specification.Engine
		if selection == nil || selection.Template != name || selection.Revision != fakeRevision(name) ||
			turn.Specification.CredentialReferences[0].Reference != "secret://kint-engine/"+name+"/gateway" {
			t.Fatalf("turn %s did not bind its own prepared template: %+v", message.ID, turn.Specification)
		}
		names[name] = true
		if prepared := templates.ensured[name]; prepared.Harness != "kint-conversation-byo" || prepared.BaseURL != "https://marina.marina-system.svc/conversation/model/v1" {
			t.Fatalf("prepared template = %+v", prepared)
		}
	}
	if len(names) != 2 || engine.starts != 2 {
		t.Fatalf("turns shared a template: names=%v starts=%d", names, engine.starts)
	}
	if strings.Join(templates.deleted, ",") != "kint-engine/"+rt.EngineTemplateName("conversation-msg_first")+",kint-engine/"+rt.EngineTemplateName("conversation-msg_second") {
		t.Fatalf("published turns kept their templates: %v", templates.deleted)
	}
}

func TestConversationProfileCannotNameAPreparedTemplate(t *testing.T) {
	model := workerapi.Model{Name: "fixture", MaxOutputTokens: 100, Currency: "USD"}
	profile := testProfile()
	if err := profile.Validate(model); err != nil {
		t.Fatal(err)
	}
	profile.Engine.Template, profile.Engine.Revision = "kint16-chat-24", strings.Repeat("a", 64)
	if err := profile.Validate(model); err == nil {
		t.Fatal("a fixed template would serve more than one turn")
	}
}
