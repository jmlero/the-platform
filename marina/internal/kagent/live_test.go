package kagent_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	api "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// This opt-in acceptance test uses the isolated KINT-02 no-inference fixture.
// The interceptor drops one *successful* create/delete response. SendMessage is
// interrupted BEFORE admission, so no model task or model credential is needed.
// Accepted-message recovery and output handling are covered by adapter_test.go.
func TestLiveIsolatedLifecycle(t *testing.T) {
	endpoint := os.Getenv("KINT03_ENDPOINT")
	revision := os.Getenv("KINT03_REVISION")
	if endpoint == "" {
		t.Skip("set KINT03_ENDPOINT and KINT03_REVISION for isolated local acceptance")
	}
	if !strings.HasPrefix(endpoint, "127.0.0.1:") {
		t.Fatal("live fixture requires an authenticated loopback Kubernetes port-forward")
	}
	selected := rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: endpoint, Namespace: "kint-engine", Harness: "kint-codex", Template: "kint-fixture", Revision: revision}
	f := setup(t, nil, selected)
	ctx := context.Background()
	createCalls, sendCalls := 0, 0
	lostDelete := false
	originalID := ""
	interceptor := func(ctx context.Context, method string, req, reply any, conn *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-user-id", "kint-03@local.invalid")
		if strings.HasSuffix(method, "/SendMessage") {
			sendCalls++
			return status.Error(codes.Unavailable, "injected interruption before message admission")
		}
		err := invoke(ctx, method, req, reply, conn, opts...)
		if strings.HasSuffix(method, "/CreateAgentInstance") && err == nil {
			createCalls++
			instance := reply.(*api.CreateAgentInstanceResponse).AgentInstance
			if originalID == "" {
				originalID = instance.Id
			} else if instance.Id != originalID {
				t.Errorf("replacement instance: %s != %s", instance.Id, originalID)
			}
			if createCalls == 1 {
				return status.Error(codes.Unavailable, "injected lost successful creation response")
			}
		}
		if strings.HasSuffix(method, "/DeleteAgentInstance") && err == nil && !lostDelete {
			lostDelete = true
			return status.Error(codes.Unavailable, "injected lost successful deletion response")
		}
		return err
	}
	reconnect := func() {
		if f.conn != nil {
			f.conn.Close()
		}
		var err error
		f.conn, err = grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(interceptor))
		if err != nil {
			t.Fatal(err)
		}
		f.store.Close()
		f.store, err = sqlite.Open(ctx, f.path)
		if err != nil {
			t.Fatal(err)
		}
		f.adapter = kagent.New(f.conn, endpoint, f.store, f.blobs, &admission{})
	}
	reconnect()
	t.Cleanup(func() { f.conn.Close() })
	t.Cleanup(func() {
		if originalID != "" {
			_, err := api.NewAgentInstanceServiceClient(f.conn).DeleteAgentInstance(ctx, &api.DeleteAgentInstanceRequest{AgentInstanceId: originalID})
			if err != nil && status.Code(err) != codes.NotFound {
				t.Errorf("trial instance cleanup: %v", err)
			}
		}
	})
	if _, err := f.adapter.Start(ctx, f.spec); err == nil {
		t.Fatal("missing injected create loss")
	}
	if originalID == "" {
		t.Fatal("real creation did not succeed")
	}
	reconnect()
	if _, err := f.adapter.InspectByKey(ctx, f.spec.IdempotencyKey); err == nil {
		t.Fatal("missing injected send interruption")
	}
	r, err := f.store.GetEngineAttempt(ctx, f.spec.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if r.InstanceID != originalID || r.Revision != revision || r.Phase != "sending" {
		t.Fatalf("live correlation incomplete: %+v", r)
	}
	t.Logf("real instance=%s revision=%s context=%s route=%s message=%s", r.InstanceID, r.Revision, r.ContextID, r.Route, r.MessageID)
	reconnect()
	for i := 0; i < 2; i++ {
		if _, err = f.adapter.InspectByKey(ctx, f.spec.IdempotencyKey); !errors.Is(err, kagent.ErrDeliveryUnknown) {
			t.Fatalf("expected inspect-only uncertain-delivery blocker: %v", err)
		}
	}
	if createCalls != 2 || sendCalls != 1 {
		t.Fatalf("duplicate work: create=%d send=%d", createCalls, sendCalls)
	}
	handle := "kagent:" + f.spec.AttemptID
	if _, err = f.adapter.Cancel(ctx, handle, true); err == nil || !lostDelete {
		t.Fatalf("real deletion did not reach injected response loss: %v", err)
	}
	reconnect()
	obs, err := f.adapter.InspectByKey(ctx, f.spec.IdempotencyKey)
	if err != nil || obs.State != rt.StateCancelled {
		t.Fatalf("cancellation after reconnect: %+v %v", obs, err)
	}
	if _, err = f.adapter.CollectArtifacts(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if err = f.adapter.Cleanup(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if _, err = f.adapter.Start(ctx, f.spec); err != nil {
		t.Fatal(err)
	}
	if createCalls != 2 || sendCalls != 1 {
		t.Fatal("replacement work after cleanup")
	}
	t.Log("lost creation response, journal/connection restart, no-resend blocker, lost deletion response and cleanup passed; zero model tasks dispatched")
}

// An accepted A2A message can lose its response after the engine persists it.
// Recovery must find that exact message in real engine history without resending.
// The isolated fixture has no working model endpoint, so this does not spend or
// prove governed model calls.
func TestLiveAcceptedMessageResponseLoss(t *testing.T) {
	endpoint := os.Getenv("KINT03_ENDPOINT")
	revision := os.Getenv("KINT03_REVISION")
	if endpoint == "" {
		t.Skip("set KINT03_ENDPOINT and KINT03_REVISION for isolated local acceptance")
	}
	if !strings.HasPrefix(endpoint, "127.0.0.1:") {
		t.Fatal("live fixture requires an authenticated loopback Kubernetes port-forward")
	}
	selected := rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: endpoint, Namespace: "kint-engine", Harness: "kint-codex", Template: "kint-fixture", Revision: revision}
	f := setup(t, nil, selected)
	ctx := context.Background()
	sends := 0
	instanceID := ""
	interceptor := func(ctx context.Context, method string, req, reply any, conn *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-user-id", "kint-09@local.invalid")
		err := invoke(ctx, method, req, reply, conn, opts...)
		if strings.HasSuffix(method, "/SendMessage") {
			sends++
			if err == nil && sends == 1 {
				return status.Error(codes.Unavailable, "injected lost accepted message response")
			}
		}
		return err
	}
	reconnect := func() {
		if f.conn != nil {
			f.conn.Close()
		}
		var err error
		f.conn, err = grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(interceptor))
		if err != nil {
			t.Fatal(err)
		}
		f.store.Close()
		f.store, err = sqlite.Open(ctx, f.path)
		if err != nil {
			t.Fatal(err)
		}
		f.adapter = kagent.New(f.conn, endpoint, f.store, f.blobs, &admission{})
	}
	reconnect()
	t.Cleanup(func() { f.conn.Close() })
	t.Cleanup(func() {
		if instanceID != "" {
			_, err := api.NewAgentInstanceServiceClient(f.conn).DeleteAgentInstance(ctx, &api.DeleteAgentInstanceRequest{AgentInstanceId: instanceID})
			if err != nil && status.Code(err) != codes.NotFound {
				t.Errorf("trial instance cleanup: %v", err)
			}
		}
	})
	if _, err := f.adapter.Start(ctx, f.spec); err == nil {
		t.Fatal("missing injected accepted-message response loss")
	}
	r, err := f.store.GetEngineAttempt(ctx, f.spec.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	instanceID = r.InstanceID
	if instanceID == "" || r.Phase != "sending" || r.TaskID != "" || sends != 1 {
		t.Fatalf("send intent was not durable: instance=%q phase=%q task=%q sends=%d", instanceID, r.Phase, r.TaskID, sends)
	}
	reconnect()
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, err = f.adapter.InspectByKey(ctx, f.spec.IdempotencyKey)
		if err == nil {
			break
		}
		if !errors.Is(err, kagent.ErrDeliveryUnknown) || time.Now().After(deadline) {
			t.Fatalf("accepted message was not recovered: %v", err)
		}
		time.Sleep(time.Second)
	}
	r, err = f.store.GetEngineAttempt(ctx, f.spec.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if r.InstanceID != instanceID || r.TaskID == "" || sends != 1 {
		t.Fatalf("recovery changed identity or resent: instance=%q task=%q sends=%d", r.InstanceID, r.TaskID, sends)
	}
	t.Logf("accepted message recovered after response loss: instance=%s task=%s message=%s sends=%d", r.InstanceID, r.TaskID, r.MessageID, sends)
}

// A template edit must not make a newly created instance eligible for a task
// that selected the previous prepared revision. Run this only after changing
// the isolated trial template and retaining the original revision in the env.
func TestLiveChangedTemplateBlocksBeforeMessage(t *testing.T) {
	if os.Getenv("KINT09_EXPECT_STALE_TEMPLATE") != "1" {
		t.Skip("set KINT09_EXPECT_STALE_TEMPLATE after editing the isolated trial template")
	}
	endpoint := os.Getenv("KINT03_ENDPOINT")
	revision := os.Getenv("KINT03_REVISION")
	if !strings.HasPrefix(endpoint, "127.0.0.1:") || revision == "" {
		t.Fatal("live fixture requires a loopback Kubernetes port-forward and the original revision")
	}
	selected := rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: endpoint, Namespace: "kint-engine", Harness: "kint-codex", Template: "kint-fixture", Revision: revision}
	f := setup(t, nil, selected)
	ctx := context.Background()
	sends := 0
	interceptor := func(ctx context.Context, method string, req, reply any, conn *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-user-id", "kint-09@local.invalid")
		if strings.HasSuffix(method, "/SendMessage") {
			sends++
		}
		return invoke(ctx, method, req, reply, conn, opts...)
	}
	var err error
	f.conn, err = grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(interceptor))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.conn.Close() })
	f.adapter = kagent.New(f.conn, endpoint, f.store, f.blobs, &admission{})
	if _, err := f.adapter.Start(ctx, f.spec); !errors.Is(err, kagent.ErrRevisionMismatch) {
		t.Fatalf("changed template was not blocked: %v", err)
	}
	r, err := f.store.GetEngineAttempt(ctx, f.spec.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if r.InstanceID == "" || r.Revision == revision || r.Phase != "revision_mismatch" || sends != 0 {
		t.Fatalf("changed template received work: phase=%s sends=%d selected=%s actual=%s", r.Phase, sends, revision, r.Revision)
	}
	t.Cleanup(func() {
		_, err := api.NewAgentInstanceServiceClient(f.conn).DeleteAgentInstance(ctx, &api.DeleteAgentInstanceRequest{AgentInstanceId: r.InstanceID})
		if err != nil && status.Code(err) != codes.NotFound {
			t.Errorf("trial instance cleanup: %v", err)
		}
	})
	t.Logf("changed prepared revision blocked before send: instance=%s selected=%s actual=%s", r.InstanceID, revision, r.Revision)
}

// A real input-required task must stop with its original instance, and an
// answer arriving after observed termination must never be sent to the engine.
func TestLiveCancelWhileWaitingForInput(t *testing.T) {
	if os.Getenv("KINT09_CANCEL_QUESTION") != "1" {
		t.Skip("set KINT09_CANCEL_QUESTION for the isolated question actor")
	}
	endpoint := os.Getenv("KINT03_ENDPOINT")
	revision := os.Getenv("KINT09_QUESTION_REVISION")
	harness := os.Getenv("KINT09_QUESTION_HARNESS")
	template := os.Getenv("KINT09_QUESTION_TEMPLATE")
	if !strings.HasPrefix(endpoint, "127.0.0.1:") || revision == "" || harness == "" || template == "" {
		t.Fatal("live question fixture requires a loopback port-forward, harness, template, and exact prepared revision")
	}
	selected := rt.EngineSelection{Backend: "kagent", Version: kagent.Version, Endpoint: endpoint,
		Namespace: "kint-engine", Harness: harness, Template: template, Revision: revision}
	f := setup(t, nil, selected)
	ctx := context.Background()
	sends := 0
	interceptor := func(ctx context.Context, method string, req, reply any, conn *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-user-id", "kint-09@local.invalid")
		if strings.HasSuffix(method, "/SendMessage") {
			sends++
		}
		return invoke(ctx, method, req, reply, conn, opts...)
	}
	var err error
	f.conn, err = grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(interceptor))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.conn.Close() })
	f.adapter = kagent.New(f.conn, endpoint, f.store, f.blobs, &admission{})
	t.Cleanup(func() {
		record, err := f.store.GetEngineAttempt(ctx, f.spec.AttemptID)
		if err != nil {
			t.Errorf("trial instance lookup: %v", err)
			return
		}
		if record.InstanceID == "" {
			return
		}
		_, err = api.NewAgentInstanceServiceClient(f.conn).DeleteAgentInstance(ctx, &api.DeleteAgentInstanceRequest{AgentInstanceId: record.InstanceID})
		if err != nil && status.Code(err) != codes.NotFound {
			t.Errorf("trial instance cleanup: %v", err)
		}
	})
	obs, err := f.adapter.Start(ctx, f.spec)
	if err != nil {
		t.Fatal(err)
	}
	record, err := f.store.GetEngineAttempt(ctx, f.spec.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	instanceID := record.InstanceID
	if instanceID == "" || record.TaskID == "" {
		t.Fatalf("question task has no durable identity: %+v", record)
	}
	deadline := time.Now().Add(90 * time.Second)
	for obs.Question == nil && time.Now().Before(deadline) {
		time.Sleep(time.Second)
		obs, err = f.adapter.Inspect(ctx, obs.ProviderHandle)
		if err != nil {
			t.Fatal(err)
		}
	}
	if obs.Question == nil || obs.Question.ID != "kint08-diagnostic-scope" || sends != 1 {
		t.Fatalf("expected one input-required task: observation=%+v sends=%d", obs, sends)
	}
	questionID := obs.Question.ID
	for {
		obs, err = f.adapter.Cancel(ctx, obs.ProviderHandle, true)
		if err == nil && obs.State == rt.StateCancelled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancellation did not observe termination: %+v %v", obs, err)
		}
		time.Sleep(time.Second)
	}
	_, err = api.NewAgentInstanceServiceClient(f.conn).GetAgentInstance(ctx, &api.GetAgentInstanceRequest{AgentInstanceId: instanceID})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("cancelled instance still exists: %v", err)
	}
	if _, err = f.adapter.AnswerQuestion(ctx, obs.ProviderHandle, questionID, "Only the frozen synthetic fixture"); !errors.Is(err, rt.ErrConflict) || sends != 1 {
		t.Fatalf("late answer reached engine: %v sends=%d", err, sends)
	}
	t.Logf("waiting task cancelled after observed deletion: instance=%s task=%s question=%s sends=%d", instanceID, record.TaskID, questionID, sends)
}
