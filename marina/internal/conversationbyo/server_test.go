package conversationbyo

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRunUsesOnlyFrozenThreadAndReturnsBoundedAnswer(t *testing.T) {
	in := Input{Version: 1, ProjectID: "prj", ChannelID: "chn", ThreadID: "thr", MessageID: "msg_2", Messages: []Message{{ID: "msg_1", Role: "human", Body: "What is a migration?"}, {ID: "msg_2", Role: "human", Body: "Can you explain further?"}}}
	in.InputDigest = DigestInput(in)
	raw, _ := json.Marshal(in)
	calls := 0
	result, err := run(context.Background(), raw, "credential", func(_ context.Context, got Input, token string) (string, error) {
		calls++
		if got.InputDigest != in.InputDigest || token != "credential" {
			t.Fatal("input or credential changed")
		}
		return "A migration changes stored data shape.", nil
	})
	if err != nil || calls != 1 || result.Answer == "" || result.MessageID != "msg_2" {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
	in.Messages[0].Body = "changed history"
	raw, _ = json.Marshal(in)
	if _, err = run(context.Background(), raw, "credential", func(context.Context, Input, string) (string, error) {
		t.Fatal("changed history reached model")
		return "", nil
	}); err == nil {
		t.Fatal("changed context accepted")
	}
}

func TestRunSeparatesDelegationInterpretationFromAuthority(t *testing.T) {
	in := Input{Version: 1, ProjectID: "prj", ChannelID: "chn", ThreadID: "thr", MessageID: "msg", Messages: []Message{{ID: "msg", Role: "human", Body: "Fix login and verify it"}}}
	in.InputDigest = DigestInput(in)
	raw, _ := json.Marshal(in)
	result, err := run(context.Background(), raw, "credential", func(context.Context, Input, string) (string, error) {
		return `{"answer":"I can take this on.","delegation":{"title":"Fix login","description":"Repair regression","acceptance_criteria":"Login test passes","mode":"ready"}}`, nil
	})
	if err != nil || result.Delegation == nil || result.Delegation.Mode != "ready" {
		t.Fatalf("structured result: %+v %v", result, err)
	}
	if _, err := run(context.Background(), raw, "credential", func(context.Context, Input, string) (string, error) {
		return `{"answer":"Doing it.","delegation":{"title":"Fix login","acceptance_criteria":"Login test passes","mode":"ready","authority":["deploy"]}}`, nil
	}); err == nil {
		t.Fatal("model-supplied authority accepted")
	}
}
