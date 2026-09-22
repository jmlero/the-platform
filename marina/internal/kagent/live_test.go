package kagent_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

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
		f.adapter = kagent.New(f.conn, endpoint, f.store, f.blobs)
	}
	reconnect()
	t.Cleanup(func() { f.conn.Close() })
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
