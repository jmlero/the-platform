package conversationbyo

import (
	"context"
	"encoding/json"
	"testing"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	hitl "github.com/kagent-dev/kagent/go/api/a2a"
)

func TestRunUsesOnlyFrozenThreadAndReturnsBoundedAnswer(t *testing.T) {
	in := Input{Version: 1, ProjectID: "prj", ChannelID: "chn", ThreadID: "thr", MessageID: "msg_2", Messages: []Message{{ID: "msg_1", Role: "human", Body: "What is a migration?"}, {ID: "msg_2", Role: "human", Body: "Can you explain further?"}}}
	in.InputDigest = DigestInput(in)
	raw, _ := json.Marshal(in)
	calls := 0
	result, _, err := run(context.Background(), raw, "credential", nil, func(_ context.Context, got Input, token string) (string, error) {
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
	if _, _, err = run(context.Background(), raw, "credential", nil, func(context.Context, Input, string) (string, error) {
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
	result, _, err := run(context.Background(), raw, "credential", nil, func(context.Context, Input, string) (string, error) {
		return `{"answer":"I can take this on.","delegation":{"title":"Fix login","description":"Repair regression","acceptance_criteria":"Login test passes","mode":"ready"}}`, nil
	})
	if err != nil || result.Delegation == nil || result.Delegation.Mode != "ready" {
		t.Fatalf("structured result: %+v %v", result, err)
	}
	if _, _, err := run(context.Background(), raw, "credential", nil, func(context.Context, Input, string) (string, error) {
		return `{"answer":"Doing it.","delegation":{"title":"Fix login","acceptance_criteria":"Login test passes","mode":"ready","authority":["deploy"]}}`, nil
	}); err == nil {
		t.Fatal("model-supplied authority accepted")
	}
}

func TestRunAsksOneClarificationThenAnswersWithIt(t *testing.T) {
	in := Input{Version: 1, ProjectID: "prj", ChannelID: "chn", ThreadID: "thr", MessageID: "msg", Messages: []Message{{ID: "msg", Role: "human", Body: "Add a status badge"}}}
	in.InputDigest = DigestInput(in)
	raw, _ := json.Marshal(in)
	result, question, err := run(context.Background(), raw, "credential", nil, func(context.Context, Input, string) (string, error) {
		return `{"answer":"One detail first.","delegation":null,"question":{"text":"Which page shows the badge?","choices":["Home","Settings"]}}`, nil
	})
	if err != nil || question == nil || question.Text != "Which page shows the badge?" || len(question.Choices) != 2 || result.Answer != "" {
		t.Fatalf("clarification: result=%+v question=%+v err=%v", result, question, err)
	}
	asked := &clarification{Question: question.Text, Answer: "Home"}
	result, question, err = run(context.Background(), raw, "credential", asked, func(_ context.Context, got Input, _ string) (string, error) {
		last := got.Messages[len(got.Messages)-2:]
		if last[0].Role != "agent" || last[0].Body != "Which page shows the badge?" || last[1].Role != "human" || last[1].Body != "Home" {
			t.Fatalf("follow-up transcript lacks the clarification: %+v", got.Messages)
		}
		// A second question is not allowed: its text becomes the reply.
		return `{"answer":"","delegation":null,"question":{"text":"Anything else?","choices":[]}}`, nil
	})
	if err != nil || question != nil || result.Answer != "Anything else?" || result.InputDigest != in.InputDigest || result.MessageID != "msg" {
		t.Fatalf("follow-up: result=%+v question=%+v err=%v", result, question, err)
	}
	if _, _, err := run(context.Background(), raw, "credential", nil, func(context.Context, Input, string) (string, error) {
		return `{"answer":"x","delegation":null,"question":{"text":"","choices":[]}}`, nil
	}); err == nil {
		t.Fatal("empty clarification question accepted")
	}
}

func TestAnswerResumesFromTheQuestionInTaskHistory(t *testing.T) {
	in := Input{Version: 1, ProjectID: "prj", ChannelID: "chn", ThreadID: "thr", MessageID: "msg", Messages: []Message{{ID: "msg", Role: "human", Body: "Add a badge"}}}
	in.InputDigest = DigestInput(in)
	raw, _ := json.Marshal(in)
	ask := hitl.AskUserRequest{Type: hitl.HITLTypeAskUserRequest, ID: questionID, Questions: []hitl.HITLQuestion{{Question: "Which page?", Choices: []string{"Home", "Settings"}}}}
	question := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Which page?"))
	if err := hitl.AttachHITL(question, ask); err != nil {
		t.Fatal(err)
	}
	answer := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Home"))
	if err := hitl.AttachHITL(answer, hitl.AskUserResponse{Type: hitl.HITLTypeAskUserResponse, ID: questionID, Answers: []hitl.AskUserAnswer{{Answer: []string{"Home"}}}}); err != nil {
		t.Fatal(err)
	}
	original := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(string(raw)))
	for name, stored := range map[string]*a2a.Task{
		"waiting":  {Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: question}, History: []*a2a.Message{original}},
		"resuming": {Status: a2a.TaskStatus{State: a2a.TaskStateWorking}, History: []*a2a.Message{original, question}},
	} {
		got, asked, ok := turnInput(&a2asrv.ExecutorContext{Message: answer, StoredTask: stored})
		if !ok || string(got) != string(raw) || asked == nil || asked.Question != "Which page?" || asked.Answer != "Home" {
			t.Fatalf("%s: resume input=%q asked=%+v ok=%v", name, got, asked, ok)
		}
	}
	other := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Settings?"))
	if err := hitl.AttachHITL(other, hitl.AskUserResponse{Type: hitl.HITLTypeAskUserResponse, ID: "another", Answers: []hitl.AskUserAnswer{{Answer: []string{"Home"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := turnInput(&a2asrv.ExecutorContext{Message: other, StoredTask: &a2a.Task{History: []*a2a.Message{original, question}}}); ok {
		t.Fatal("an answer to another question resumed the turn")
	}
}

func TestRunDraftsPRDWithoutDelegatingExecution(t *testing.T) {
	in := Input{Version: 1, ProjectID: "prj", ChannelID: "chn", ThreadID: "thr", MessageID: "msg", Messages: []Message{{ID: "msg", Role: "human", Body: "Draft a PRD for filters"}}}
	in.InputDigest = DigestInput(in)
	raw, _ := json.Marshal(in)
	result, _, err := run(context.Background(), raw, "credential", nil, func(context.Context, Input, string) (string, error) {
		return `{"answer":"Review the draft.","prd":{"title":"Filters","path":"docs/prd/filters.md","content":"# Filters\n\nSave filters.\n"}}`, nil
	})
	if err != nil || result.PRD == nil || result.Delegation != nil {
		t.Fatalf("draft: %+v %v", result, err)
	}
	for _, path := range []string{"../AGENTS.md", "docs/prd/../../AGENTS.md", "/docs/prd/file.md", "docs/prd/file.md\n"} {
		result.PRD.Path = path
		if ValidateResult(result) == nil {
			t.Fatalf("unsafe PRD path %q", path)
		}
	}
	result.PRD.Path = "docs/prd/filters.md"
	result.Delegation = &Delegation{Title: "Publish", AcceptanceCriteria: "Landed", Mode: "ready"}
	if ValidateResult(result) == nil {
		t.Fatal("draft granted execution")
	}
}
