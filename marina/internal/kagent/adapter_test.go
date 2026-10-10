package kagent_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2a "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	api "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type engine struct {
	api.UnimplementedAgentInstanceServiceServer
	a2a.UnimplementedA2AServiceServer
	instance                                                                       *api.AgentInstance
	task                                                                           *a2a.Task
	creates, sends, deletes                                                        int
	loseCreate, loseSend, loseDelete, hideTask, failDelete, loseAnswer, holdAnswer bool
	revision                                                                       string
	// immediate records each send's ReturnImmediately flag; a subscription lets
	// the engine record the task's latest state, as the pinned controller does.
	immediate     []bool
	subscriptions int
	finishOnWatch bool
	// stallAnswer models the pinned controller: an answer sent with
	// ReturnImmediately leaves the stored task waiting for input.
	stallAnswer      bool
	unavailableWatch bool
}

func (e *engine) SubscribeToTask(r *a2a.SubscribeToTaskRequest, stream grpc.ServerStreamingServer[a2a.StreamResponse]) error {
	e.subscriptions++
	if e.unavailableWatch {
		return status.Error(codes.Unavailable, "worker no longer reachable")
	}
	if e.task == nil || r.Id != e.task.Id {
		return status.Error(codes.NotFound, "no task")
	}
	if e.finishOnWatch {
		e.task.Status.State = a2a.TaskState_TASK_STATE_COMPLETED
	}
	return stream.Send(&a2a.StreamResponse{Payload: &a2a.StreamResponse_Task{Task: e.task}})
}

func TestLostComputeStopsAfterBoundedObservationsWithoutReplacementWork(t *testing.T) {
	e := &engine{unavailableWatch: true}
	f := setup(t, e)
	ctx := context.Background()
	obs, err := f.adapter.Start(ctx, f.spec)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		obs, err = f.adapter.Inspect(ctx, obs.ProviderHandle)
		if err != nil {
			t.Fatal(err)
		}
	}
	if obs.State != rt.StateCancelled || e.instance != nil || e.sends != 1 || e.creates != 1 {
		t.Fatalf("lost worker still running or replaced: %+v creates=%d sends=%d", obs, e.creates, e.sends)
	}
}

func TestRetiredDiagnosticCannotStartOnNativeEngine(t *testing.T) {
	e := &engine{}
	f := setup(t, e)
	f.spec.Workload = "diagnostic-analysis/v1"
	if _, err := f.adapter.Start(context.Background(), f.spec); err == nil || !strings.Contains(err.Error(), "retired") || e.creates != 0 || e.sends != 0 {
		t.Fatalf("retired workload reached execution: %v creates=%d sends=%d", err, e.creates, e.sends)
	}
}

type admission struct {
	err   error
	calls int
}

func (g *admission) AdmitEngine(context.Context, rt.Specification) error { g.calls++; return g.err }

func TestDispatchRequiresCurrentAdmissionIncludingRecovery(t *testing.T) {
	e := &engine{loseCreate: true}
	f := setup(t, e)
	g := &admission{err: errors.New("revoked")}
	f.adapter = kagent.New(f.conn, "test-engine", f.store, f.blobs, g)
	if _, err := f.adapter.Start(context.Background(), f.spec); err == nil || e.creates != 0 {
		t.Fatal("denied creation reached engine")
	}
	g.err = nil
	f.adapter.Start(context.Background(), f.spec)
	g.err = errors.New("new fence")
	if _, err := f.adapter.InspectByKey(context.Background(), f.spec.IdempotencyKey); err == nil || e.creates != 1 || e.sends != 0 {
		t.Fatal("recovery bypassed current admission")
	}
	// Missing admission also fails closed; cancellation must still be available.
	f.adapter = kagent.New(f.conn, "test-engine", f.store, f.blobs, nil)
	if _, err := f.adapter.InspectByKey(context.Background(), f.spec.IdempotencyKey); err == nil {
		t.Fatal("missing admission accepted")
	}
	if _, err := f.adapter.Cancel(context.Background(), "kagent:"+f.spec.AttemptID, false); err != nil {
		t.Fatal(err)
	}
}

func (e *engine) CreateAgentInstance(_ context.Context, r *api.CreateAgentInstanceRequest) (*api.CreateAgentInstanceResponse, error) {
	e.creates++
	if e.instance == nil {
		e.instance = &api.AgentInstance{Id: "instance-1", Harness: r.Harness, AgentTemplate: r.AgentTemplate, PreparedRevision: e.revision, ContextId: "context-1", A2AAuthority: "recorded-route", State: api.AgentInstanceState_AGENT_INSTANCE_STATE_READY}
	}
	if e.loseCreate {
		e.loseCreate = false
		return nil, status.Error(codes.Unavailable, "response lost")
	}
	return &api.CreateAgentInstanceResponse{AgentInstance: e.instance}, nil
}
func (e *engine) GetAgentInstance(context.Context, *api.GetAgentInstanceRequest) (*api.GetAgentInstanceResponse, error) {
	if e.instance == nil {
		return nil, status.Error(codes.NotFound, "gone")
	}
	return &api.GetAgentInstanceResponse{AgentInstance: e.instance}, nil
}
func (e *engine) DeleteAgentInstance(context.Context, *api.DeleteAgentInstanceRequest) (*api.DeleteAgentInstanceResponse, error) {
	e.deletes++
	if e.failDelete {
		return nil, status.Error(codes.Unavailable, "still running")
	}
	e.instance = nil
	if e.loseDelete {
		e.loseDelete = false
		return nil, status.Error(codes.Unavailable, "response lost")
	}
	return &api.DeleteAgentInstanceResponse{AgentInstance: &api.AgentInstance{State: api.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED}}, nil
}
func (e *engine) SendMessage(_ context.Context, r *a2a.SendMessageRequest) (*a2a.SendMessageResponse, error) {
	e.sends++
	e.immediate = append(e.immediate, r.GetConfiguration().GetReturnImmediately())
	if r.Message.TaskId != "" {
		e.task.History = append(e.task.History, r.Message)
		switch {
		case e.stallAnswer && r.GetConfiguration().GetReturnImmediately():
			// The stored task keeps waiting for input.
		case e.holdAnswer:
			e.task.Status.State = a2a.TaskState_TASK_STATE_WORKING
		default:
			e.task.Status.State = a2a.TaskState_TASK_STATE_COMPLETED
		}
		if e.loseAnswer {
			e.loseAnswer = false
			return nil, status.Error(codes.Unavailable, "answer response lost")
		}
		return &a2a.SendMessageResponse{Payload: &a2a.SendMessageResponse_Task{Task: e.task}}, nil
	}
	e.task = &a2a.Task{Id: "task-1", ContextId: r.Message.ContextId, History: []*a2a.Message{r.Message}, Status: &a2a.TaskStatus{State: a2a.TaskState_TASK_STATE_WORKING}}
	if e.loseSend {
		return nil, status.Error(codes.Unavailable, "response lost")
	}
	return &a2a.SendMessageResponse{Payload: &a2a.SendMessageResponse_Task{Task: e.task}}, nil
}
func TestDuplicateAnswerIsRejectedWhileEngineContinues(t *testing.T) {
	e := &engine{holdAnswer: true}
	f := setup(t, e)
	ctx := context.Background()
	obs, err := f.adapter.Start(ctx, f.spec)
	if err != nil {
		t.Fatal(err)
	}
	statusMessage := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Answer required"))
	if err := kagenta2a.AttachHITL(statusMessage, kagenta2a.AskUserRequest{Type: kagenta2a.HITLTypeAskUserRequest,
		ID: "question-1", Questions: []kagenta2a.HITLQuestion{{Question: "Which database?", Choices: []string{"SQLite", "PostgreSQL"}}}}); err != nil {
		t.Fatal(err)
	}
	converted, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: statusMessage})
	if err != nil {
		t.Fatal(err)
	}
	e.task.Status.State = a2a.TaskState_TASK_STATE_INPUT_REQUIRED
	e.task.Status.Message = converted.Message
	obs, err = f.adapter.Inspect(ctx, obs.ProviderHandle)
	if err != nil || obs.Question == nil {
		t.Fatalf("question = %+v, %v", obs, err)
	}
	obs, err = f.adapter.AnswerQuestion(ctx, obs.ProviderHandle, "question-1", "SQLite")
	if err != nil || obs.State != rt.StateRunning || e.sends != 2 {
		t.Fatalf("answer = %+v, %v; sends=%d", obs, err, e.sends)
	}
	if _, err = f.adapter.AnswerQuestion(ctx, obs.ProviderHandle, "question-1", "SQLite"); !errors.Is(err, rt.ErrConflict) || e.sends != 2 {
		t.Fatalf("duplicate answer accepted: %v; sends=%d", err, e.sends)
	}
}

func TestAnsweredTaskEndsEvenWhenTheAnswerResponseIsLost(t *testing.T) {
	// The pinned controller records what follows an answer only when the answer
	// was not sent with ReturnImmediately, even if the response is then lost.
	e := &engine{loseAnswer: true, stallAnswer: true}
	f := setup(t, e)
	ctx := context.Background()
	obs, err := f.adapter.Start(ctx, f.spec)
	if err != nil {
		t.Fatal(err)
	}
	statusMessage := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Answer required"))
	if err := kagenta2a.AttachHITL(statusMessage, kagenta2a.AskUserRequest{Type: kagenta2a.HITLTypeAskUserRequest,
		ID: "question-1", Questions: []kagenta2a.HITLQuestion{{Question: "Which database?", Choices: []string{"SQLite", "PostgreSQL"}}}}); err != nil {
		t.Fatal(err)
	}
	converted, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: statusMessage})
	if err != nil {
		t.Fatal(err)
	}
	e.task.Status.State = a2a.TaskState_TASK_STATE_INPUT_REQUIRED
	e.task.Status.Message = converted.Message
	if obs, err = f.adapter.Inspect(ctx, obs.ProviderHandle); err != nil || obs.Question == nil {
		t.Fatalf("question = %+v, %v", obs, err)
	}
	if _, err = f.adapter.AnswerQuestion(ctx, obs.ProviderHandle, "question-1", "SQLite"); err == nil {
		t.Fatal("lost answer response reported delivery")
	}
	if obs, err = f.adapter.Inspect(ctx, obs.ProviderHandle); err != nil || obs.State != rt.StateSucceeded || e.sends != 2 || e.immediate[1] {
		t.Fatalf("answered task = %+v %v sends=%d immediate=%v", obs, err, e.sends, e.immediate)
	}
}

func TestExactPendingQuestionAnswerRecoversLostResponseWithoutResend(t *testing.T) {
	e := &engine{loseAnswer: true}
	f := setup(t, e)
	ctx := context.Background()
	obs, err := f.adapter.Start(ctx, f.spec)
	if err != nil {
		t.Fatal(err)
	}
	statusMessage := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Answer required"))
	if err := kagenta2a.AttachHITL(statusMessage, kagenta2a.AskUserRequest{Type: kagenta2a.HITLTypeAskUserRequest,
		ID: "question-1", Questions: []kagenta2a.HITLQuestion{{Question: "Which database?", Choices: []string{"SQLite", "PostgreSQL"}}}}); err != nil {
		t.Fatal(err)
	}
	converted, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: statusMessage})
	if err != nil {
		t.Fatal(err)
	}
	e.task.Status.State = a2a.TaskState_TASK_STATE_INPUT_REQUIRED
	e.task.Status.Message = converted.Message
	obs, err = f.adapter.Inspect(ctx, obs.ProviderHandle)
	if err != nil || obs.Question == nil || obs.Question.ID != "question-1" || obs.Question.Text != "Which database?" {
		t.Fatalf("pending question = %+v, %v", obs, err)
	}
	if _, err = f.adapter.AnswerQuestion(ctx, obs.ProviderHandle, "wrong-question", "SQLite"); err == nil || e.sends != 1 {
		t.Fatal("stale question was sent")
	}
	if _, err = f.adapter.AnswerQuestion(ctx, obs.ProviderHandle, "question-1", "SQLite"); err == nil || e.sends != 2 {
		t.Fatalf("lost answer response = %v sends=%d", err, e.sends)
	}
	f.restart(t)
	if _, err = f.adapter.AnswerQuestion(ctx, obs.ProviderHandle, "question-1", "PostgreSQL"); !errors.Is(err, rt.ErrConflict) || e.sends != 2 {
		t.Fatalf("changed answer after lost response accepted: %v sends=%d", err, e.sends)
	}
	obs, err = f.adapter.Inspect(ctx, obs.ProviderHandle)
	if err != nil || obs.State != rt.StateSucceeded || e.sends != 2 {
		t.Fatalf("answer recovery = %+v, %v sends=%d", obs, err, e.sends)
	}
}
func (e *engine) ListTasks(context.Context, *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	r := &a2a.ListTasksResponse{}
	if e.task != nil && !e.hideTask {
		r.Tasks = []*a2a.Task{e.task}
	}
	return r, nil
}
func (e *engine) GetTask(context.Context, *a2a.GetTaskRequest) (*a2a.Task, error) {
	if e.task == nil {
		return nil, status.Error(codes.NotFound, "gone")
	}
	return e.task, nil
}

type fixture struct {
	adapter *kagent.Adapter
	store   *sqlite.Store
	spec    rt.Specification
	path    string
	conn    *grpc.ClientConn
	blobs   *artifacts.Store
}

func setup(t *testing.T, e *engine, selection ...rt.EngineSelection) *fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	f := &fixture{path: filepath.Join(dir, "journal.db")}
	var err error
	f.store, err = sqlite.Open(ctx, f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.store.Close() })
	f.blobs, err = artifacts.New(filepath.Join(dir, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := f.blobs.Put([]byte(`{"task":"synthetic lifecycle test"}`))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ex, _, err := f.store.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "work", ExternalRunID: "run", AgentID: "marina", LeaseExpiresAt: now.Add(time.Hour), Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 2, MaxTokens: 100}})
	if err != nil {
		t.Fatal(err)
	}
	s := rt.Specification{IdempotencyKey: "start-key", RepositoryURI: "https://example.invalid/repo", SourceRevision: "abc", ContextURI: "artifact://" + digest, ContextDigest: "sha256:" + strings.Repeat("c", 64), ProfileDigest: strings.Repeat("a", 64), HarnessVersion: "fixture/v1", PolicyVersion: "policy/v1", AuthorityFence: ex.FenceToken, DeadlineAt: now.Add(time.Minute * 30), NetworkPolicy: "isolated", FilesystemPolicy: "workspace", Limits: rt.Limits{CPUMillis: 1, MemoryBytes: 1, EphemeralStorageBytes: 1, WallTimeSeconds: 60, MaxProcesses: 1}, Engine: &rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: "test-engine", Namespace: "trial", Harness: "codex", Template: "fixture", Revision: strings.Repeat("b", 64)}}
	if len(selection) > 0 {
		s.Engine = &selection[0]
		s.IdempotencyKey = fmt.Sprintf("kint-03-%d", time.Now().UnixNano())
	}
	if e != nil {
		e.revision = s.Engine.Revision
	}
	_, attempt, _, err := f.store.EnsureRuntimeAttempt(ctx, ex.ID, ex.Version, s)
	if err != nil {
		t.Fatal(err)
	}
	f.spec = attempt.Specification
	if e == nil {
		return f
	}
	listener := bufconn.Listen(8 << 20)
	server := grpc.NewServer()
	api.RegisterAgentInstanceServiceServer(server, e)
	a2a.RegisterA2AServiceServer(server, e)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	f.conn, err = grpc.NewClient("passthrough:///engine", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.conn.Close() })
	f.adapter = kagent.New(f.conn, "test-engine", f.store, f.blobs, &admission{})
	return f
}
func (f *fixture) restart(t *testing.T) {
	t.Helper()
	f.store.Close()
	var err error
	f.store, err = sqlite.Open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.adapter = kagent.New(f.conn, "test-engine", f.store, f.blobs, &admission{})
}

func TestLostCreationAndMessageResponsesRecoverOriginalAttemptAfterRestart(t *testing.T) {
	e := &engine{loseCreate: true, loseSend: true}
	f := setup(t, e)
	ctx := context.Background()
	if _, err := f.adapter.Start(ctx, f.spec); err == nil {
		t.Fatal("wanted uncertain create")
	}
	f.restart(t)
	if _, err := f.adapter.InspectByKey(ctx, f.spec.IdempotencyKey); err == nil {
		t.Fatal("wanted uncertain message")
	}
	f.restart(t)
	obs, err := f.adapter.InspectByKey(ctx, f.spec.IdempotencyKey)
	if err != nil || obs.State != rt.StateRunning {
		t.Fatalf("recovery: %+v %v", obs, err)
	}
	if _, err = f.adapter.Start(ctx, f.spec); err != nil {
		t.Fatal(err)
	}
	if e.creates != 2 || e.sends != 1 {
		t.Fatalf("duplicate work: creates=%d sends=%d", e.creates, e.sends)
	}
	record, err := f.store.GetEngineAttempt(ctx, f.spec.AttemptID)
	if err != nil || record.TaskID != "task-1" || record.ContextID != "context-1" || record.Route != "recorded-route" {
		t.Fatalf("correlation: %+v %v", record, err)
	}
}
func TestUnknownDeliveryNeverResends(t *testing.T) {
	e := &engine{loseSend: true, hideTask: true}
	f := setup(t, e)
	ctx := context.Background()
	f.adapter.Start(ctx, f.spec)
	f.restart(t)
	for i := 0; i < 2; i++ {
		if _, err := f.adapter.InspectByKey(ctx, f.spec.IdempotencyKey); !errors.Is(err, kagent.ErrDeliveryUnknown) {
			t.Fatalf("want truthful blocker: %v", err)
		}
	}
	if e.sends != 1 {
		t.Fatal("message was resent")
	}
}
func TestChangedTemplateRevisionCannotReceiveWork(t *testing.T) {
	e := &engine{}
	f := setup(t, e)
	e.revision = strings.Repeat("c", 64)
	if _, err := f.adapter.Start(context.Background(), f.spec); !errors.Is(err, kagent.ErrRevisionMismatch) {
		t.Fatalf("revision error: %v", err)
	}
	f.restart(t)
	f.adapter.InspectByKey(context.Background(), f.spec.IdempotencyKey)
	if e.sends != 0 {
		t.Fatal("unauthorized revision received work")
	}
}
func TestCancellationWaitsForDeletionAndRecoversLostResponse(t *testing.T) {
	e := &engine{failDelete: true}
	f := setup(t, e)
	ctx := context.Background()
	obs, err := f.adapter.Start(ctx, f.spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.adapter.Cancel(ctx, obs.ProviderHandle, true); err == nil {
		t.Fatal("premature cancellation")
	}
	f.restart(t)
	e.failDelete = false
	e.loseDelete = true
	if _, err = f.adapter.Cancel(ctx, obs.ProviderHandle, true); err == nil {
		t.Fatal("wanted lost delete response")
	}
	f.restart(t)
	obs, err = f.adapter.InspectByKey(ctx, f.spec.IdempotencyKey)
	if err != nil || obs.State != rt.StateCancelled {
		t.Fatalf("cancellation recovery: %+v %v", obs, err)
	}
	if e.creates != 1 || e.sends != 1 {
		t.Fatal("replacement work during cancellation")
	}
}

func TestCleanupPreservesArtifactsAcrossRestartAndNeverRecreatesInstance(t *testing.T) {
	e := &engine{}
	f := setup(t, e)
	ctx := context.Background()
	obs, err := f.adapter.Start(ctx, f.spec)
	if err != nil {
		t.Fatal(err)
	}
	e.task.Status.State = a2a.TaskState_TASK_STATE_COMPLETED
	e.task.Artifacts = []*a2a.Artifact{{ArtifactId: "output", Parts: []*a2a.Part{{Content: &a2a.Part_Text{Text: "unverified result"}}}}}
	obs, err = f.adapter.Inspect(ctx, obs.ProviderHandle)
	if err != nil || obs.State != rt.StateSucceeded {
		t.Fatalf("completion observation: %+v %v", obs, err)
	}
	if err = f.adapter.Cleanup(ctx, obs.ProviderHandle); err == nil {
		t.Fatal("deleted uncollected results")
	}
	outputs, err := f.adapter.CollectArtifacts(ctx, obs.ProviderHandle)
	if err != nil || len(outputs) != 1 {
		t.Fatalf("outputs: %+v %v", outputs, err)
	}
	original := outputs[0]
	bytes, err := f.blobs.Get(original.Digest)
	if err != nil || !strings.Contains(string(bytes), "unverified result") {
		t.Fatalf("artifact: %s %v", bytes, err)
	}
	if err = f.adapter.Cleanup(ctx, obs.ProviderHandle); err != nil {
		t.Fatal(err)
	}
	f.restart(t)
	outputs, err = f.adapter.CollectArtifacts(ctx, obs.ProviderHandle)
	if err != nil || len(outputs) != 1 || outputs[0] != original {
		t.Fatalf("retained outputs: %+v %v", outputs, err)
	}
	if _, err = f.adapter.Start(ctx, f.spec); err != nil {
		t.Fatal(err)
	}
	if e.creates != 1 || e.sends != 1 {
		t.Fatal("recreated deleted instance")
	}
}

func TestCancellationDoesNotDeleteChangedInstanceIdentity(t *testing.T) {
	e := &engine{}
	f := setup(t, e)
	ctx := context.Background()
	obs, err := f.adapter.Start(ctx, f.spec)
	if err != nil {
		t.Fatal(err)
	}
	e.instance.ContextId = "other-context"
	if _, err = f.adapter.Cancel(ctx, obs.ProviderHandle, true); !errors.Is(err, kagent.ErrIdentity) {
		t.Fatalf("wanted identity blocker: %v", err)
	}
	if e.deletes != 0 {
		t.Fatal("deleted an unexpected instance")
	}
}

func TestCorrelationRejectsIdentityMutationAndStaleWrites(t *testing.T) {
	e := &engine{}
	f := setup(t, e)
	ctx := context.Background()
	if _, err := f.adapter.Start(ctx, f.spec); err != nil {
		t.Fatal(err)
	}
	r, err := f.store.GetEngineAttempt(ctx, f.spec.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	changed := r
	changed.TaskID = "another-task"
	if _, err = f.store.UpdateEngineAttempt(ctx, changed); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("identity mutation: %v", err)
	}
	if _, err = f.store.UpdateEngineAttempt(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.UpdateEngineAttempt(ctx, r); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("stale write: %v", err)
	}
	s := f.spec
	copy := *s.Engine
	copy.Revision = strings.Repeat("d", 64)
	s.Engine = &copy
	if _, err = f.adapter.Start(ctx, s); !errors.Is(err, execution.ErrConflict) {
		t.Fatalf("spec mutation: %v", err)
	}
}

func TestCancellationOfUncertainCreationNeverSendsWork(t *testing.T) {
	e := &engine{loseCreate: true}
	f := setup(t, e)
	ctx := context.Background()
	f.adapter.Start(ctx, f.spec)
	f.restart(t)
	obs, err := f.adapter.Cancel(ctx, "kagent:"+f.spec.AttemptID, false)
	if err != nil || obs.State != rt.StateCancelled || e.sends != 0 || e.instance != nil {
		t.Fatalf("uncertain creation cancellation: %+v %v sends=%d", obs, err, e.sends)
	}
}

func TestMissingBoundInstanceIsABlockerWithoutRecreation(t *testing.T) {
	e := &engine{}
	f := setup(t, e)
	ctx := context.Background()
	obs, err := f.adapter.Start(ctx, f.spec)
	if err != nil {
		t.Fatal(err)
	}
	e.instance = nil
	f.restart(t)
	if _, err = f.adapter.Inspect(ctx, obs.ProviderHandle); !errors.Is(err, kagent.ErrIdentity) {
		t.Fatalf("missing instance: %v", err)
	}
	if e.creates != 1 || e.sends != 1 {
		t.Fatal("recreated missing work")
	}
}

func TestEngineCancelledStatusAloneDoesNotProveTermination(t *testing.T) {
	e := &engine{failDelete: true}
	f := setup(t, e)
	ctx := context.Background()
	obs, err := f.adapter.Start(ctx, f.spec)
	if err != nil {
		t.Fatal(err)
	}
	e.task.Status.State = a2a.TaskState_TASK_STATE_CANCELED
	obs, err = f.adapter.Inspect(ctx, obs.ProviderHandle)
	if err == nil || obs.State == rt.StateCancelled || e.deletes != 1 {
		t.Fatalf("must attempt observed termination: %+v %v deletes=%d", obs, err, e.deletes)
	}
	e.failDelete = false
	obs, err = f.adapter.InspectByKey(ctx, f.spec.IdempotencyKey)
	if err != nil || obs.State != rt.StateCancelled {
		t.Fatalf("termination recovery: %+v %v", obs, err)
	}
}

func TestPendingInteractionRetainsExactTaskIdentityWithoutResume(t *testing.T) {
	e := &engine{loseSend: true}
	f := setup(t, e)
	ctx := context.Background()
	f.adapter.Start(ctx, f.spec)
	e.task.Status.State = a2a.TaskState_TASK_STATE_INPUT_REQUIRED
	f.restart(t)
	if _, err := f.adapter.InspectByKey(ctx, f.spec.IdempotencyKey); err == nil {
		t.Fatal("unsupported interaction did not block")
	}
	r, err := f.store.GetEngineAttempt(ctx, f.spec.AttemptID)
	if err != nil || r.TaskID != "task-1" {
		t.Fatalf("lost pending task identity: %+v %v", r, err)
	}
	if _, err = f.adapter.Resume(ctx, "kagent:"+f.spec.AttemptID); !errors.Is(err, rt.ErrUnsupportedResume) {
		t.Fatal("resume was advertised")
	}
	if f.adapter.Capabilities().Resume || e.sends != 1 {
		t.Fatal("interaction expanded runtime capabilities")
	}
}

func TestLongEngineTaskIsFollowedUntilTheEngineRecordsItsEnd(t *testing.T) {
	// The pinned controller records a task's progress only while something
	// subscribes; a send that waits for the end outlives the caller's deadline.
	e := &engine{}
	f := setup(t, e)
	observed, err := f.adapter.Start(context.Background(), f.spec)
	if err != nil || observed.State != rt.StateRunning {
		t.Fatalf("start = %+v, %v", observed, err)
	}
	if len(e.immediate) != 1 || !e.immediate[0] || e.subscriptions != 1 {
		t.Fatalf("send waited or was not followed: immediate=%v subscriptions=%d", e.immediate, e.subscriptions)
	}
	if observed, err = f.adapter.Inspect(context.Background(), observed.ProviderHandle); err != nil || observed.State != rt.StateRunning || e.subscriptions != 2 {
		t.Fatalf("working task not followed: %+v %v subscriptions=%d", observed, err, e.subscriptions)
	}
	e.finishOnWatch = true
	if _, err = f.adapter.Inspect(context.Background(), observed.ProviderHandle); err != nil || e.subscriptions != 3 {
		t.Fatalf("follow = %v subscriptions=%d", err, e.subscriptions)
	}
	observed, err = f.adapter.Inspect(context.Background(), observed.ProviderHandle)
	if err != nil || observed.State != rt.StateSucceeded || e.subscriptions != 3 || e.sends != 1 {
		t.Fatalf("recorded end = %+v %v subscriptions=%d sends=%d", observed, err, e.subscriptions, e.sends)
	}
}
