package runtime

import (
	"strings"
	"testing"
	"time"
)

func TestConversationSpecificationHasNoRepositoryOrTools(t *testing.T) {
	now := time.Now().UTC()
	spec := Specification{
		Workload: "conversation/v1", ExecutionID: "msg_1", AttemptID: "rtm_1", IdempotencyKey: "conversation-msg-1",
		InputDigest: strings.Repeat("a", 64), ProfileDigest: strings.Repeat("b", 64),
		ContextURI: "artifact://sha256:" + strings.Repeat("c", 64), ContextDigest: "sha256:" + strings.Repeat("c", 64),
		HarnessVersion: "conversation-byo/v1", NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral",
		Limits:         Limits{CPUMillis: 100, MemoryBytes: 256 << 20, EphemeralStorageBytes: 256 << 20, WallTimeSeconds: 60, MaxProcesses: 16},
		AuthorityFence: 1, DeadlineAt: now.Add(time.Minute), PolicyVersion: "test",
		Engine:               &EngineSelection{Backend: "kagent", Version: "pinned", Endpoint: "engine", Namespace: "test", Harness: "conversation", Template: "turn-1", Revision: strings.Repeat("d", 64)},
		CredentialReferences: []CredentialReference{{Kind: "engine-gateway", Reference: "secret://test/turn-1/gateway", ExpiresAt: now.Add(time.Minute)}},
	}
	if err := spec.Validate(now); err != nil {
		t.Fatalf("read-only conversation rejected: %v", err)
	}
	for _, change := range []func(*Specification){
		func(s *Specification) { s.RepositoryURI = "https://git.example/repo" },
		func(s *Specification) { s.SourceRevision = strings.Repeat("e", 40) },
		func(s *Specification) { s.Tools = []string{"read_context_source"} },
	} {
		changed := spec
		change(&changed)
		if err := changed.Validate(now); err == nil {
			t.Fatal("conversation acquired source or tool authority")
		}
	}
}
