package flow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workresult"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestEngineResultTranslationRequiresSelectedVerifiedBytes(t *testing.T) {
	ctx := context.Background()
	head := strings.Repeat("a", 40)
	for name, mutate := range map[string]func(*a2a.Artifact, *workresult.Result){
		"valid":       func(*a2a.Artifact, *workresult.Result) {},
		"native text": func(a *a2a.Artifact, _ *workresult.Result) { a.Name = "" },
		"missing inline bytes": func(a *a2a.Artifact, _ *workresult.Result) {
			a.Parts[0].Content = &a2a.Part_Url{Url: "https://example.invalid/result"}
		},
		"wrong revision":  func(_ *a2a.Artifact, r *workresult.Result) { r.SourceRevision = strings.Repeat("b", 40) },
		"disallowed path": func(_ *a2a.Artifact, r *workresult.Result) { r.Changes[0].Path = "other/main.go" },
	} {
		t.Run(name, func(t *testing.T) {
			journal, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "journal.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			store, err := artifacts.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			e, _, err := journal.EnsureExecution(ctx, execution.NewExecution{WorkProvider: "deck", ExternalWorkID: "task", ExternalRunID: "run", AgentID: "agent", LeaseExpiresAt: now.Add(time.Minute), Budget: execution.Budget{DeadlineAt: now.Add(time.Hour), MaxAttempts: 2, MaxTokens: 10000}})
			if err != nil {
				t.Fatal(err)
			}
			_, attempt, _, err := journal.EnsureRuntimeAttempt(ctx, e.ID, e.Version, runtimeapi.Specification{IdempotencyKey: "engine", RepositoryURI: "https://git.example/repo.git", SourceRevision: head, ContextURI: "journal://context", ContextDigest: "sha256:context", HarnessVersion: "test", Limits: runtimeapi.Limits{CPUMillis: 1000, MemoryBytes: 1024, EphemeralStorageBytes: 1024, WallTimeSeconds: 60, MaxProcesses: 256}, NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: now.Add(time.Minute), PolicyVersion: "test"})
			if err != nil {
				t.Fatal(err)
			}
			result := workresult.Result{Stage: "implement", SourceRevision: head, ObservedAt: now, Changes: []workresult.Change{{Path: "app/main.go", Mode: "100644", Content: []byte("safe")}}}
			artifact := &a2a.Artifact{Name: "marina-result.json", Parts: []*a2a.Part{{MediaType: "application/json", Content: &a2a.Part_Raw{}}}}
			mutate(artifact, &result)
			payload, _ := json.Marshal(result)
			if _, ok := artifact.Parts[0].Content.(*a2a.Part_Raw); ok {
				if name == "native text" {
					artifact.Parts = []*a2a.Part{{Content: &a2a.Part_Text{Text: string(payload[:len(payload)/2])}}, {Content: &a2a.Part_Text{Text: string(payload[len(payload)/2:])}}}
				} else {
					artifact.Parts[0].Content = &a2a.Part_Raw{Raw: payload}
				}
			}
			envelope, _ := protojson.Marshal(artifact)
			digest, err := store.Put(envelope)
			if err != nil {
				t.Fatal(err)
			}
			item, _, err := journal.RecordRuntimeArtifact(ctx, runtimeapi.Artifact{ID: "engine-output", ExecutionID: e.ID, RuntimeAttemptID: attempt.ID, Name: "kagent-output-0.json", Kind: "kagent-a2a-output", URI: "artifact://" + digest, Digest: digest, SizeBytes: int64(len(envelope))})
			if err != nil {
				t.Fatal(err)
			}
			controller := &Controller{Journal: journal, Artifacts: store}
			want := workresult.Expected{Stage: "implement", SourceRevision: head, AllowedPaths: []string{"app"}, NotBefore: attempt.CreatedAt.Add(-time.Minute), Now: now}
			got, err := controller.translateEngineResult(ctx, attempt, []runtimeapi.Artifact{item}, want)
			if name == "valid" || name == "native text" {
				if err != nil || got.Digest == "" || got.Result.Changes[0].Path != "app/main.go" {
					t.Fatalf("valid conversion: %+v %v", got, err)
				}
				loaded, err := journal.ListRuntimeArtifacts(ctx, e.ID)
				if err != nil || len(loaded) != 2 || loaded[0].Digest != got.Digest {
					t.Fatalf("digest-bound result: %+v %v", loaded, err)
				}
				if _, err := controller.translateEngineResult(ctx, attempt, nil, want); err == nil {
					t.Fatal("missing result accepted")
				}
				if _, err := controller.translateEngineResult(ctx, attempt, []runtimeapi.Artifact{item, item}, want); err == nil {
					t.Fatal("duplicate result accepted")
				}
				wrongSize := item
				wrongSize.SizeBytes++
				if _, err := controller.translateEngineResult(ctx, attempt, []runtimeapi.Artifact{wrongSize}, want); err == nil {
					t.Fatal("false artifact size accepted")
				}
			} else if err == nil {
				t.Fatal("unsafe engine result accepted")
			}
		})
	}
}
