package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/conversationbyo"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"google.golang.org/protobuf/encoding/protojson"
)

type testSource struct {
	message   workcontrol.ConversationMessage
	invokedAt int64
}

func (s testSource) ListConversationInvocations(_ context.Context, _ string, afterID int64) ([]workcontrol.ConversationInvocation, error) {
	if afterID > 0 {
		return nil, nil
	}
	invokedAt := s.invokedAt
	if invokedAt == 0 {
		invokedAt = s.message.CreatedAt
	}
	return []workcontrol.ConversationInvocation{{EventID: 1, InvokedAt: invokedAt, Message: s.message}}, nil
}

func (s testSource) PageConversationMessages(_ context.Context, _ string, cursor string) (workcontrol.ConversationMessagePage, error) {
	if cursor != "" {
		return workcontrol.ConversationMessagePage{}, nil
	}
	return workcontrol.ConversationMessagePage{Items: []workcontrol.ConversationMessage{s.message}}, nil
}
func (s testSource) EnsureConversationQuestion(context.Context, workcontrol.ConversationQuestionInput) (workcontrol.ConversationQuestion, error) {
	return workcontrol.ConversationQuestion{}, nil
}
func (s testSource) PublishConversationOutcome(_ context.Context, in workcontrol.ConversationOutcomeInput) (workcontrol.ConversationOutcome, error) {
	return workcontrol.ConversationOutcome{InvocationMessageID: in.InvocationMessageID, MessageID: "msg_published", Status: in.Status, ResultDigest: in.ResultDigest}, nil
}

type testEngine struct {
	starts int
	spec   rt.Specification
	blobs  *artifacts.Store
	result conversationbyo.Result
}

type publicationSource struct {
	testSource
	calls int
	seen  workcontrol.ConversationOutcomeInput
	lost  bool
}

func (s *publicationSource) PublishConversationOutcome(_ context.Context, in workcontrol.ConversationOutcomeInput) (workcontrol.ConversationOutcome, error) {
	s.calls++
	if s.calls > 1 && in.Body != s.seen.Body {
		return workcontrol.ConversationOutcome{}, errors.New("publication intent changed on retry")
	}
	s.seen = in
	if s.lost && s.calls == 1 {
		return workcontrol.ConversationOutcome{}, errors.New("response lost after Deck committed")
	}
	out := workcontrol.ConversationOutcome{InvocationMessageID: in.InvocationMessageID, MessageID: "msg_published", Status: in.Status, ResultDigest: in.ResultDigest, FollowupTaskID: in.FollowupTaskID}
	if in.Task != nil {
		out.TaskID = "tsk_published"
	}
	return out, nil
}

type questionSource struct {
	testSource
	answered bool
	seen     int
}

func (s *questionSource) EnsureConversationQuestion(_ context.Context, in workcontrol.ConversationQuestionInput) (workcontrol.ConversationQuestion, error) {
	s.seen++
	q := workcontrol.ConversationQuestion{ID: "cq_1", ProjectID: s.message.ProjectID, ChannelID: s.message.ChannelID, ThreadID: s.message.ThreadID, InvocationMessageID: s.message.ID, AgentID: s.message.InvokedAgentID, QuestionID: in.QuestionID, Status: "pending"}
	if s.answered {
		q.Status, q.Answer, q.AnswerMessageID = "answered", "GitHub", "msg_answer"
	}
	return q, nil
}

type questionEngine struct {
	*testEngine
	answered int
}

func (e *questionEngine) Start(_ context.Context, spec rt.Specification) (rt.Observation, error) {
	e.spec = spec
	e.starts++
	return rt.Observation{Found: true, ProviderHandle: "kagent:" + spec.AttemptID, State: rt.StateRunning, Question: &rt.Question{ID: "provider", Text: "Which provider?"}}, kagent.ErrInteractionRequired
}
func (e *questionEngine) AnswerQuestion(_ context.Context, handle, questionID, answer string) (rt.Observation, error) {
	if handle != "kagent:"+e.spec.AttemptID || questionID != "provider" || answer != "GitHub" {
		return rt.Observation{}, rt.ErrConflict
	}
	e.answered++
	return rt.Observation{Found: true, ProviderHandle: handle, State: rt.StateSucceeded}, nil
}

func (e *testEngine) Start(_ context.Context, s rt.Specification) (rt.Observation, error) {
	e.starts++
	e.spec = s
	return rt.Observation{Found: true, ProviderHandle: "kagent:" + s.AttemptID, State: rt.StateSucceeded}, nil
}
func (e *testEngine) Inspect(context.Context, string) (rt.Observation, error) {
	return rt.Observation{}, nil
}
func (e *testEngine) InspectByKey(context.Context, string) (rt.Observation, error) {
	return rt.Observation{}, nil
}
func (e *testEngine) Resume(context.Context, string) (rt.Observation, error) {
	return rt.Observation{}, rt.ErrUnsupportedResume
}
func (e *testEngine) Cancel(_ context.Context, h string, _ bool) (rt.Observation, error) {
	return rt.Observation{Found: true, ProviderHandle: h, State: rt.StateCancelled}, nil
}
func (e *testEngine) CollectArtifacts(context.Context, string) ([]rt.Artifact, error) {
	selected := e.result
	if selected.Answer == "" {
		selected = conversationbyo.Result{Answer: "A bounded reply"}
	}
	selected.Version, selected.MessageID, selected.ThreadID, selected.InputDigest = 1, e.spec.AttemptID, "thr_1", e.spec.InputDigest
	result, _ := json.Marshal(selected)
	envelope, _ := protojson.Marshal(&a2a.Artifact{Name: "marina-conversation-result.json", Parts: []*a2a.Part{{Content: &a2a.Part_Raw{Raw: result}, MediaType: "application/json"}}})
	digest, err := e.blobs.Put(envelope)
	return []rt.Artifact{{ID: "result", RuntimeAttemptID: e.spec.AttemptID, Kind: "kagent-a2a-output", Digest: digest, URI: "artifact://" + digest, SizeBytes: int64(len(envelope))}}, err
}
func (e *testEngine) Cleanup(context.Context, string) error { return nil }
func (e *testEngine) Capabilities() rt.Capabilities         { return rt.Capabilities{} }

func TestDispatchOfOlderMessageUsesInvocationDeadlineAndDurableEventCursor(t *testing.T) {
	ctx := context.Background()
	journal, err := sqlite.Open(ctx, t.TempDir()+"/journal.db")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	invokedAt := time.Now().UnixMilli()
	message := workcontrol.ConversationMessage{ID: "msg_old", ProjectID: "prj_1", ChannelID: "chn_1", ThreadID: "thr_1", AuthorType: "human", Body: "Explain this", InvokedAgentID: "agt_1", InvocationRequestID: "req_old", CreatedAt: invokedAt - int64(time.Hour/time.Millisecond)}
	controller := Controller{ProjectID: "prj_1", AgentID: "agt_1", Journal: journal, Source: testSource{message: message, invokedAt: invokedAt}, Profile: Profile{BudgetMinutes: 5, MaxTokens: 5000}}
	if err := controller.scanInvocations(ctx); err != nil {
		t.Fatal(err)
	}
	turn, err := journal.GetConversationTurn(ctx, message.ID)
	if err != nil || turn.DeadlineAt.UnixMilli() != invokedAt+int64(5*time.Minute/time.Millisecond) {
		t.Fatalf("invocation deadline: %+v %v", turn, err)
	}
	if err := controller.scanInvocations(ctx); err != nil {
		t.Fatal(err)
	}
	cursor, err := journal.GetConversationDispatchCursor(ctx, "prj_1")
	if err != nil || cursor != 1 {
		t.Fatalf("durable event cursor: %d %v", cursor, err)
	}
}

func TestInvocationRunsOnceWithReadOnlyEngineSpecification(t *testing.T) {
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
	message := workcontrol.ConversationMessage{ID: "msg_1", ProjectID: "prj_1", ChannelID: "chn_1", ThreadID: "thr_1", AuthorType: "human", Body: "Explain this", InvokedAgentID: "agt_1", InvocationRequestID: "req_1", CreatedAt: time.Now().UnixMilli()}
	engine := &testEngine{blobs: blobs}
	model := workerapi.Model{Name: "fixture", MaxOutputTokens: 100, Currency: "USD"}
	key := []byte(strings.Repeat("k", 32))
	profile := Profile{Engine: rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-conversation-byo", Template: "turn-1", Revision: strings.Repeat("a", 64)}, MaxTokens: 5000, BudgetMinutes: 5}
	changedModel := model
	changedModel.Endpoint = "https://other-model.example/v1"
	if profile.Digest(changedModel) == profile.Digest(model) {
		t.Fatal("model endpoint change did not change the authorized profile")
	}
	controller := Controller{ProjectID: "prj_1", AgentID: "agt_1", PolicyVersion: "test", Source: testSource{message: message}, Journal: journal, Blobs: blobs, Engine: engine,
		Profile: profile,
		Model:   model, Gateway: NewGateway(journal, key, model, profile.Digest(model), "test", nil), InstallCredential: func(context.Context, rt.Specification, string) error { return nil }}
	if failures := controller.Cycle(ctx); len(failures) != 0 {
		t.Fatal(failures)
	}
	if failures := controller.Cycle(ctx); len(failures) != 0 {
		t.Fatal(failures)
	}
	turn, err := journal.GetConversationTurn(ctx, message.ID)
	if err != nil || turn.Status != "completed" || turn.ResultDigest == "" || engine.starts != 1 {
		t.Fatalf("turn=%+v starts=%d err=%v", turn, engine.starts, err)
	}
	if engine.spec.RepositoryURI != "" || engine.spec.SourceRevision != "" || len(engine.spec.Tools) != 0 || engine.spec.Workload != "conversation/v1" {
		t.Fatalf("source authority in conversation spec: %+v", engine.spec)
	}
}

func TestDelegatedResultRetriesOneFrozenPublicationAfterLostResponse(t *testing.T) {
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
	message := workcontrol.ConversationMessage{ID: "msg_delegate", ProjectID: "prj_1", ChannelID: "chn_1", ThreadID: "thr_1", AuthorType: "human", Body: "Fix login", InvokedAgentID: "agt_1", InvocationRequestID: "req_delegate", CreatedAt: time.Now().UnixMilli()}
	source := &publicationSource{testSource: testSource{message: message}, lost: true}
	engine := &testEngine{blobs: blobs, result: conversationbyo.Result{Answer: "I will handle it.", Delegation: &conversationbyo.Delegation{Title: "Fix login", Description: "Repair login", AcceptanceCriteria: "Regression passes", Mode: "ready"}}}
	model := workerapi.Model{Name: "fixture", MaxOutputTokens: 100, Currency: "USD"}
	profile := Profile{Engine: rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-conversation-byo", Template: "turn-delegate", Revision: strings.Repeat("a", 64)}, MaxTokens: 5000, BudgetMinutes: 5,
		Delegation: &workcontrol.ConversationBudget{TimeMinutes: 30, MaxAttempts: 2, MaxTokens: 20000, MaxConcurrency: 1, Authority: []string{"source.read", "source.write", "pull_request.create"}}}
	tooManyAttempts := profile
	copyBudget := *profile.Delegation
	copyBudget.MaxAttempts = 3
	tooManyAttempts.Delegation = &copyBudget
	if err := tooManyAttempts.Validate(model); err == nil {
		t.Fatal("three-attempt conversation grant exceeds the repository workflow")
	}
	controller := Controller{ProjectID: "prj_1", AgentID: "agt_1", PolicyVersion: "test", Source: source, Journal: journal, Blobs: blobs, Engine: engine,
		Profile: profile, Model: model, DelegationProfileDigest: strings.Repeat("b", 64), Gateway: NewGateway(journal, []byte(strings.Repeat("k", 32)), model, profile.Digest(model), "test", nil), InstallCredential: func(context.Context, rt.Specification, string) error { return nil }}
	if failures := controller.Cycle(ctx); len(failures) != 1 {
		t.Fatalf("lost response should leave terminal unpublished: %v", failures)
	}
	turn, err := journal.GetConversationTurn(ctx, message.ID)
	if err != nil || turn.Status != "completed" || turn.PublishedMessageID != "" || turn.PublicationJSON == "" {
		t.Fatalf("publication intent: %+v %v", turn, err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	controller.Journal = reopened
	controller.Profile.Delegation = nil // a changed local policy cannot alter the recorded request
	if failures := controller.Cycle(ctx); len(failures) != 0 {
		t.Fatalf("publication recovery: %v", failures)
	}
	turn, err = reopened.GetConversationTurn(ctx, message.ID)
	if err != nil || turn.PublishedMessageID != "msg_published" || turn.PublishedTaskID != "tsk_published" || source.calls != 2 || engine.starts != 1 || source.seen.Task == nil || source.seen.Task.Status != "ready" {
		t.Fatalf("recovered outcome: turn=%+v calls=%d starts=%d seen=%+v err=%v", turn, source.calls, engine.starts, source.seen, err)
	}
}

func TestExplicitDraftRequestCannotBecomeReadyFromModelMode(t *testing.T) {
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
	message := workcontrol.ConversationMessage{ID: "msg_draft", ProjectID: "prj_1", ChannelID: "chn_1", ThreadID: "thr_1", AuthorType: "human", Body: "Create a Backlog draft task to improve the status page. Keep it a draft and do not start implementation.", InvokedAgentID: "agt_1", InvocationRequestID: "req_draft", CreatedAt: time.Now().UnixMilli()}
	source := &publicationSource{testSource: testSource{message: message}}
	engine := &testEngine{blobs: blobs, result: conversationbyo.Result{Answer: "I will record the idea.", Delegation: &conversationbyo.Delegation{Title: "Improve status page", AcceptanceCriteria: "Blocked tasks have a clear explanation", Mode: "ready"}}}
	model := workerapi.Model{Name: "fixture", MaxOutputTokens: 100, Currency: "USD"}
	profile := Profile{Engine: rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-conversation-byo", Template: "turn-draft", Revision: strings.Repeat("a", 64)}, MaxTokens: 5000, BudgetMinutes: 5,
		Delegation: &workcontrol.ConversationBudget{TimeMinutes: 30, MaxAttempts: 2, MaxTokens: 20000, MaxConcurrency: 1, Authority: []string{"source.read", "source.write", "pull_request.create"}}}
	c := Controller{ProjectID: "prj_1", AgentID: "agt_1", PolicyVersion: "test", Source: source, Journal: journal, Blobs: blobs, Engine: engine,
		Profile: profile, Model: model, DelegationProfileDigest: strings.Repeat("b", 64), Gateway: NewGateway(journal, []byte(strings.Repeat("k", 32)), model, profile.Digest(model), "test", nil), InstallCredential: func(context.Context, rt.Specification, string) error { return nil }}
	if failures := c.Cycle(ctx); len(failures) != 0 {
		t.Fatal(failures)
	}
	if source.seen.Task == nil || source.seen.Task.Status != "backlog" || source.seen.Task.Budget != nil || !strings.Contains(source.seen.Body, "Backlog draft created") {
		t.Fatalf("explicit draft was promoted: %+v", source.seen)
	}
}

func TestEngineQuestionWaitsThenResumesOriginatingConversation(t *testing.T) {
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
	message := workcontrol.ConversationMessage{ID: "msg_1", ProjectID: "prj_1", ChannelID: "chn_1", ThreadID: "thr_1", AuthorType: "human", Body: "Help me choose", InvokedAgentID: "agt_1", InvocationRequestID: "req_1", CreatedAt: time.Now().UnixMilli()}
	source := &questionSource{testSource: testSource{message: message}}
	engine := &questionEngine{testEngine: &testEngine{blobs: blobs}}
	model := workerapi.Model{Name: "fixture", MaxOutputTokens: 100, Currency: "USD"}
	profile := Profile{Engine: rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "kint-engine-controller.kint-engine.svc:8083", Namespace: "kint-engine", Harness: "kint-conversation-byo", Template: "turn-question", Revision: strings.Repeat("a", 64)}, MaxTokens: 5000, BudgetMinutes: 5}
	c := Controller{ProjectID: "prj_1", AgentID: "agt_1", PolicyVersion: "test", Source: source, Journal: journal, Blobs: blobs, Engine: engine, Profile: profile, Model: model, Gateway: NewGateway(journal, []byte(strings.Repeat("k", 32)), model, profile.Digest(model), "test", nil), InstallCredential: func(context.Context, rt.Specification, string) error { return nil }}
	if failures := c.Cycle(ctx); len(failures) != 0 {
		t.Fatal(failures)
	}
	turn, err := journal.GetConversationTurn(ctx, message.ID)
	if err != nil || turn.Status != "running" || source.seen != 1 || engine.answered != 0 {
		t.Fatalf("question wait: %+v %d %d %v", turn, source.seen, engine.answered, err)
	}
	source.answered = true
	if failures := c.Cycle(ctx); len(failures) != 0 {
		t.Fatal(failures)
	}
	turn, err = journal.GetConversationTurn(ctx, message.ID)
	if err != nil || turn.Status != "completed" || engine.answered != 1 || turn.ResultDigest == "" {
		t.Fatalf("question resume: %+v %d %v", turn, engine.answered, err)
	}
}
