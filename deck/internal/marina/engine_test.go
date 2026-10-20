package marina

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
)

const landedCommit = "7c1e0f2a9b8d4c3e5f6a7b8c9d0e1f2a3b4c5d6e"

type harness struct {
	t       *testing.T
	ctx     context.Context
	store   *store.Store
	engine  *Engine
	fake    *fakeMarina
	project store.ProjectChannel
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(dir, "deck.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	project, err := s.CreateProjectChannel(ctx, "jmlero/marina-acceptance", "main")
	if err != nil {
		t.Fatal(err)
	}
	fake, server := newFakeMarina(t)
	client, err := New(Config{URL: server.URL, TokenFile: tokenFile(t, "deck-token")})
	if err != nil {
		t.Fatal(err)
	}
	projects := filepath.Join(dir, "projects.json")
	mapping, _ := json.Marshal([]ProjectMapping{{DeckProject: project.Project.ID, Namespace: "factory", Project: "acceptance",
		Workloads: map[string]string{"repository/v2": "land-on-main"}, ConversationWorkload: "chat"}})
	if err := os.WriteFile(projects, mapping, 0o600); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, ctx: ctx, store: s, fake: fake, project: project,
		engine: &Engine{Store: s, Client: client, ProjectsFile: projects, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	h.cycle()
	return h
}

func (h *harness) cycle() {
	h.t.Helper()
	if err := h.engine.Cycle(h.ctx); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) readyTask(title string) domain.Task {
	h.t.Helper()
	task, err := h.store.CreateTask(h.ctx, domain.CreateTaskInput{ProjectID: h.project.Project.ID, Title: title, AcceptanceCriteria: "A unit test covers it",
		Status: domain.TaskReady, Budget: &domain.Budget{TimeMinutes: 30, MaxTokens: 10000, MaxAttempts: 3, Authority: []string{"source.read", "source.write", "source.fast_forward"}}})
	if err != nil {
		h.t.Fatal(err)
	}
	return task
}

func (h *harness) detail(id string) domain.TaskDetail {
	h.t.Helper()
	detail, err := h.store.GetTaskDetail(h.ctx, id)
	if err != nil {
		h.t.Fatal(err)
	}
	return detail
}

func succeed(run *fakeRun) {
	run.Run.Status.Phase = "Succeeded"
	run.Run.Status.Landing = &struct {
		Commit     string `json:"commit"`
		ObservedAt string `json:"observedAt"`
	}{Commit: landedCommit}
	for _, kind := range []string{"review", "verification", "ci", "landing"} {
		run.Run.Status.Evidence = append(run.Run.Status.Evidence, Evidence{ID: run.Run.Metadata.Name + "-" + kind, Kind: kind, Passed: true, Summary: kind + " passed", SourceRevision: landedCommit})
	}
}

func TestProjectSyncPublishesTheBoundRepositoryAndWorkloadProfiles(t *testing.T) {
	h := newHarness(t)
	repository, err := h.store.GetRepository(h.ctx, h.project.Project.ID)
	if err != nil || repository.Status != "connected" || repository.Binding == nil || repository.Binding.Repository != "jmlero/marina-acceptance" || repository.Binding.Version != 3 {
		t.Fatalf("repository = %+v, %v", repository, err)
	}
	catalog, err := h.store.GetWorkloads(h.ctx, h.project.Project.ID)
	if err != nil || len(catalog.Profiles) != 1 || catalog.Profiles[0].ID != "repository/v2" || catalog.Profiles[0].MaxTokens != 400000 ||
		catalog.Profiles[0].Digest != "1b4f0e9851971998e732078544c96b36c3d01cedf7caa332359d6f1d83567014" || catalog.Profiles[0].Authority[2] != "source.fast_forward" {
		t.Fatalf("catalog = %+v, %v", catalog, err)
	}
	agent, err := h.store.GetAgent(h.ctx, store.MarinaAgentID)
	if err != nil || agent.Capabilities[0] != "conversation.invoke."+h.project.Project.ID {
		t.Fatalf("agent = %+v, %v", agent, err)
	}
}

func TestReadyTaskBecomesOneRunThatIsApprovedVerifiedAndLanded(t *testing.T) {
	h := newHarness(t)
	task := h.readyTask("Reject an empty range")
	h.cycle()
	name, run := h.fake.onlyRun()
	var spec RunSpec
	if err := json.Unmarshal(run.Spec, &spec); err != nil || spec.Project != "acceptance" || spec.Workload != "land-on-main" || spec.Title != task.Title ||
		spec.Budget.MaxTokens != 10000 || spec.Budget.MaxAttempts != 3 || !strings.HasPrefix(name, "deck-tsk-") {
		t.Fatalf("spec = %s (%s)", run.Spec, name)
	}
	if h.detail(task.ID).Task.Status != domain.TaskPlanning {
		t.Fatal("task did not start planning")
	}
	h.fake.update(func() { run.Run.Status.Phase = "WaitingDecision" })
	h.fake.decide(name, "Run", name+"-plan-1", "plan-approval", "Approve plan", "Request changes")
	h.cycle()
	detail := h.detail(task.ID)
	if detail.Task.Status != domain.TaskWaitingDecision || len(detail.Decisions) != 1 || detail.Decisions[0].ID != name+"-plan-1" ||
		detail.PlanRevisions[0].Plan != "Add a guard and one unit test." {
		t.Fatalf("waiting = %+v %+v", detail.Task.Status, detail.Decisions)
	}
	// An answer given with another client is mirrored, then the run starts.
	h.fake.update(func() {
		h.fake.decisions[name+"-plan-1"].Spec.Response = &struct {
			Answer     string `json:"answer"`
			AnsweredBy string `json:"answeredBy"`
		}{Answer: "Approve plan"}
		run.Run.Status.Phase = "Running"
		run.Events = append(run.Events, Event{Sequence: 1, Type: "run.accepted", Message: "Run accepted"},
			Event{Sequence: 2, Type: "execution.progress", Message: "Implementing the approved change", Fields: map[string]any{"step": "implement"}})
	})
	h.cycle()
	if status := h.detail(task.ID).Task.Status; status != domain.TaskRunning {
		t.Fatalf("status = %s", status)
	}
	h.fake.update(func() { succeed(run) })
	h.cycle()
	h.cycle()
	detail = h.detail(task.ID)
	if detail.Task.Status != domain.TaskDone || detail.Task.Landing == nil || detail.Task.Landing.Commit != landedCommit || len(detail.Evidence) != 4 {
		t.Fatalf("done = %+v landing %+v evidence %d", detail.Task.Status, detail.Task.Landing, len(detail.Evidence))
	}
	progress := 0
	for _, event := range detail.Events {
		if event.Type == "execution.progress" {
			progress++
		}
		if event.Type == "run.accepted" {
			t.Fatal("a lifecycle event was copied as well as recorded")
		}
	}
	h.cycle() // An idle replay changes nothing.
	if replay := h.detail(task.ID); progress != 1 || h.fake.creates != 1 || replay.Task.Version != detail.Task.Version || len(replay.Events) != len(detail.Events) {
		t.Fatalf("progress %d creates %d", progress, h.fake.creates)
	}
}

func TestALostCreateResponseIsReplayedWithoutASecondRun(t *testing.T) {
	h := newHarness(t)
	h.fake.loseCreates = 1
	task := h.readyTask("Survive a lost response")
	h.cycle()
	h.cycle()
	name, _ := h.fake.onlyRun()
	if h.fake.creates != 2 {
		t.Fatalf("creates = %d", h.fake.creates)
	}
	// A restarted Deck continues from its ledger: the same Run, no new one.
	h.engine = &Engine{Store: h.store, Client: h.engine.Client, ProjectsFile: h.engine.ProjectsFile, Log: h.engine.Log}
	h.cycle()
	if again, _ := h.fake.onlyRun(); again != name || h.fake.creates != 2 || h.detail(task.ID).Runs[0].ID != name {
		t.Fatalf("after restart: %s creates %d", again, h.fake.creates)
	}
}

func TestBlockedFailedAndRejectedRunsShowMarinasReason(t *testing.T) {
	h := newHarness(t)
	blocked := h.readyTask("Please block this")
	h.cycle()
	_, run := h.fake.onlyRun()
	h.fake.update(func() {
		run.Run.Status.Phase, run.Run.Status.Reason = "Blocked", "Sandbox block requested by the title"
	})
	h.cycle()
	detail := h.detail(blocked.ID)
	if detail.Task.Status != domain.TaskBlocked || detail.Runs[0].ResultSummary != "Sandbox block requested by the title" {
		t.Fatalf("blocked = %s %q", detail.Task.Status, detail.Runs[0].ResultSummary)
	}
	h.fake.rejectCreate = "budget.maxTokens exceeds the installation ceiling"
	rejected := h.readyTask("Too large")
	h.cycle()
	detail = h.detail(rejected.ID)
	if detail.Task.Status != domain.TaskBlocked || !strings.Contains(detail.Runs[0].ResultSummary, "installation ceiling") {
		t.Fatalf("rejected = %s %q", detail.Task.Status, detail.Runs[0].ResultSummary)
	}
}

func TestCancellationInMarinaCompletesTheOperatorsCancelRequest(t *testing.T) {
	h := newHarness(t)
	task := h.readyTask("Cancel me")
	h.cycle()
	name, run := h.fake.onlyRun()
	if _, err := h.store.CreateCommand(h.ctx, name, domain.CommandCancel, "Stop", "operator:local"); err != nil {
		t.Fatal(err)
	}
	h.fake.update(func() { run.Run.Status.Phase = "Cancelled" })
	h.cycle()
	detail := h.detail(task.ID)
	if detail.Task.Status != domain.TaskCancelled || len(detail.Commands) != 1 || detail.Commands[0].Status != "completed" {
		t.Fatalf("cancelled = %s %+v", detail.Task.Status, detail.Commands)
	}
}

func TestWorkloadsWhoseDeliveryDoesNotMatchTheProfileReceiveNoWork(t *testing.T) {
	h := newHarness(t)
	h.fake.update(func() {
		h.fake.workloads["land-on-main"] = []byte(strings.Replace(string(h.fake.workloads["land-on-main"]), `"mode":"land-on-main"`, `"mode":"pull-request"`, 1))
	})
	h.engine.synced = nil
	h.cycle()
	task := h.readyTask("Must not land through a pull-request workload")
	h.cycle()
	if len(h.fake.runs) != 0 || h.detail(task.ID).Task.Status != domain.TaskReady {
		t.Fatal("work was submitted to a mismatched workload")
	}
}

func TestTasksWithoutAMappedWorkloadStayReady(t *testing.T) {
	h := newHarness(t)
	task, err := h.store.CreateTask(h.ctx, domain.CreateTaskInput{ProjectID: h.project.Project.ID, Title: "Pull request work", AcceptanceCriteria: "c",
		Status: domain.TaskReady, Budget: &domain.Budget{MaxAttempts: 2, MaxTokens: 1000, Authority: []string{"source.read", "source.write", "pull_request.create"}}})
	if err != nil {
		t.Fatal(err)
	}
	h.cycle()
	if len(h.fake.runs) != 0 || h.detail(task.ID).Task.Status != domain.TaskReady {
		t.Fatal("work without a mapped workload was submitted")
	}
}

func TestChatTurnsReplyAskAndProposeInTheThread(t *testing.T) {
	h := newHarness(t)
	channel := h.project.Channels[0].ID
	invoke := func(body string) domain.ConversationInvocation {
		invocation, err := h.store.InvokeConversation(h.ctx, domain.ConversationInvocationInput{ChannelID: channel, AgentID: store.MarinaAgentID, Body: body, RequestID: "r-" + body, AuthorID: "operator:local"})
		if err != nil {
			t.Fatal(err)
		}
		return invocation
	}
	messages := func(thread string) []string {
		page, err := h.store.PageThreadMessages(h.ctx, thread, 50, "")
		if err != nil {
			t.Fatal(err)
		}
		var bodies []string
		for _, message := range page.Items {
			bodies = append(bodies, message.AuthorType+": "+message.Body)
		}
		return bodies
	}
	hello := invoke("Hello")
	h.cycle()
	name := store.MarinaName(hello.Message.ID, "")
	turn := h.fake.turns[name]
	if turn == nil || turn.Spec.Workload != "chat" || turn.Spec.Thread.ID != hello.Thread.ID || len(turn.Spec.Thread.Messages) != 1 || turn.Spec.Thread.Messages[0].Body != "Hello" || turn.Spec.Budget.MaxTokens != 40000 {
		t.Fatalf("turn = %+v", turn)
	}
	h.fake.update(func() {
		turn.Turn.Status.Phase = "Succeeded"
		turn.Turn.Status.Reply = &struct {
			Answer string `json:"answer"`
		}{Answer: "Sandbox reply to: Hello"}
	})
	h.cycle()
	h.cycle()
	if got := messages(hello.Thread.ID); len(got) != 2 || got[1] != "agent: Sandbox reply to: Hello" {
		t.Fatalf("thread = %v", got)
	}

	clarify := invoke("Please clarify the design")
	h.cycle()
	turnName := store.MarinaName(clarify.Message.ID, "")
	h.fake.decide(turnName, "Turn", turnName+"-q", "clarification", "API", "Store")
	h.cycle()
	questions, err := h.store.ListConversationQuestions(h.ctx, clarify.Thread.ID)
	if err != nil || len(questions) != 1 || questions[0].QuestionID != turnName+"-q" || questions[0].Choices[1] != "Store" {
		t.Fatalf("questions = %+v, %v", questions, err)
	}

	propose := invoke("Please propose a task")
	h.cycle()
	proposed := h.fake.turns[store.MarinaName(propose.Message.ID, "")]
	h.fake.update(func() {
		proposed.Turn.Status.Phase = "Succeeded"
		proposed.Turn.Status.Reply = &struct {
			Answer string `json:"answer"`
		}{Answer: "Here is a task"}
		proposed.Turn.Status.Proposals = &struct {
			Task *TaskProposal `json:"task"`
			PRD  *struct {
				Path    string `json:"path"`
				Title   string `json:"title"`
				Content string `json:"content"`
			} `json:"prd"`
			PRDTasks *struct {
				Source string         `json:"source"`
				Tasks  []TaskProposal `json:"tasks"`
			} `json:"prdTasks"`
		}{Task: &TaskProposal{Title: "Sandbox proposal", AcceptanceCriteria: "A test passes"}}
	})
	h.cycle()
	tasks, err := h.store.ListTasks(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range tasks {
		found = found || task.Title == "Sandbox proposal" && task.Status == domain.TaskBacklog
	}
	if !found {
		t.Fatalf("no backlog proposal in %+v", tasks)
	}

	failed := invoke("Break")
	h.cycle()
	broken := h.fake.turns[store.MarinaName(failed.Message.ID, "")]
	h.fake.update(func() { broken.Turn.Status.Phase, broken.Turn.Status.Reason = "Failed", "model unavailable" })
	h.cycle()
	if got := messages(failed.Thread.ID); len(got) != 2 || !strings.Contains(got[1], "model unavailable") {
		t.Fatalf("failed thread = %v", got)
	}
}

func (h *harness) providerProblem() string {
	h.t.Helper()
	status, err := h.store.GetProviderStatus(h.ctx, h.project.Project.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return status.LastError
}

func TestARunWithoutPlanApprovalStartsEvenWhenDeckMissedPlanningAndMarinaAsked(t *testing.T) {
	h := newHarness(t)
	task := h.readyTask("Ask while Deck is away")
	h.cycle()
	name, run := h.fake.onlyRun()
	// While Deck was away, Marina planned without approval, ran and asked.
	h.fake.update(func() { run.Run.Status.Phase = "Running" })
	h.fake.decide(name, "Run", name+"-q", "clarification", "API", "Store")
	h.cycle()
	detail := h.detail(task.ID)
	if detail.Task.Status != domain.TaskRunning || len(detail.Decisions) != 1 || detail.Decisions[0].InteractionID != name+"-q" {
		t.Fatalf("status = %s decisions %+v", detail.Task.Status, detail.Decisions)
	}
}

func TestACancelBeforeMarinaConfirmedTheRunNeverCreatesIt(t *testing.T) {
	h := newHarness(t)
	h.fake.refuseCreate = 503
	task := h.readyTask("Cancel while Marina is unavailable")
	h.cycle()
	run := h.detail(task.ID).Runs[0]
	if _, err := h.store.CreateCommand(h.ctx, run.ID, domain.CommandCancel, "Not needed", "operator:local"); err != nil {
		t.Fatal(err)
	}
	h.fake.refuseCreate = 0
	creates := h.fake.creates
	h.cycle()
	h.cycle()
	detail := h.detail(task.ID)
	if detail.Task.Status != domain.TaskCancelled || len(h.fake.runs) != 0 || h.fake.creates != creates || detail.Commands[0].Status != "completed" {
		t.Fatalf("status %s runs %d creates %d commands %+v", detail.Task.Status, len(h.fake.runs), h.fake.creates-creates, detail.Commands)
	}
}

func TestACancelAfterALostCreateResponseIsForwardedToMarina(t *testing.T) {
	h := newHarness(t)
	h.fake.loseCreates = 2 // the dispatch and the retry in the same cycle
	task := h.readyTask("Cancel after a lost response")
	h.cycle()
	name, run := h.fake.onlyRun()
	if _, err := h.store.CreateCommand(h.ctx, name, domain.CommandCancel, "Not needed", "operator:local"); err != nil {
		t.Fatal(err)
	}
	h.cycle()
	if len(h.fake.cancels) != 1 || h.fake.cancels[0] != name+" deck-cancel-"+name || h.fake.creates != 2 {
		t.Fatalf("cancels %v creates %d", h.fake.cancels, h.fake.creates)
	}
	h.fake.update(func() { run.Run.Status.Phase = "Cancelled" })
	h.cycle()
	if detail := h.detail(task.ID); detail.Task.Status != domain.TaskCancelled || detail.Commands[0].Status != "completed" {
		t.Fatalf("status %s commands %+v", detail.Task.Status, detail.Commands)
	}
}

func TestARunIsGoneOnlyWhenMarinaNamesItMissingAndItsProjectAnswers(t *testing.T) {
	h := newHarness(t)
	task := h.readyTask("Survive a misrouted Marina")
	h.cycle()
	name, _ := h.fake.onlyRun()
	h.fake.update(func() { h.fake.plainNotFound = true })
	h.cycle()
	if status := h.detail(task.ID).Task.Status; status != domain.TaskPlanning || h.providerProblem() == "" {
		t.Fatalf("a bare 404 ended the run: %s %q", status, h.providerProblem())
	}
	h.fake.update(func() { h.fake.plainNotFound, h.fake.projectDown = false, true; delete(h.fake.runs, name) })
	h.cycle()
	if status := h.detail(task.ID).Task.Status; status != domain.TaskPlanning {
		t.Fatalf("a missing run with an unreachable project ended the run: %s", status)
	}
	h.fake.update(func() { h.fake.projectDown = false })
	h.cycle()
	if status := h.detail(task.ID).Task.Status; status != domain.TaskFailed {
		t.Fatalf("status = %s", status)
	}
}

func TestAForbiddenCreateIsRetriedAndShownInTheProviderStatus(t *testing.T) {
	h := newHarness(t)
	h.fake.refuseCreate = 403
	task := h.readyTask("Wait for RBAC")
	h.cycle()
	if status := h.detail(task.ID).Task.Status; status != domain.TaskPlanning || !strings.Contains(h.providerProblem(), "Forbidden") {
		t.Fatalf("status %s problem %q", status, h.providerProblem())
	}
	h.fake.refuseCreate = 0
	h.cycle()
	if _, run := h.fake.onlyRun(); run == nil || h.providerProblem() != "" {
		t.Fatalf("problem %q", h.providerProblem())
	}
}

func TestAForbiddenChatTurnIsRetried(t *testing.T) {
	h := newHarness(t)
	h.fake.refuseCreate = 403
	invocation, err := h.store.InvokeConversation(h.ctx, domain.ConversationInvocationInput{ChannelID: h.project.Channels[0].ID, AgentID: store.MarinaAgentID, Body: "Hello", RequestID: "r-hello", AuthorID: "operator:local"})
	if err != nil {
		t.Fatal(err)
	}
	h.cycle()
	h.fake.refuseCreate = 0
	h.cycle()
	if h.fake.turns[store.MarinaName(invocation.Message.ID, "")] == nil {
		t.Fatal("the chat turn was not retried")
	}
}

func TestAnInvalidProjectsFileKeepsTheLastValidMapping(t *testing.T) {
	h := newHarness(t)
	task := h.readyTask("Keep mirroring")
	h.cycle()
	_, run := h.fake.onlyRun()
	if err := os.WriteFile(h.engine.ProjectsFile, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.fake.update(func() { run.Run.Status.Phase = "Cancelled" })
	h.cycle()
	if status := h.detail(task.ID).Task.Status; status != domain.TaskCancelled || !strings.Contains(h.providerProblem(), "projects file") {
		t.Fatalf("status %s problem %q", status, h.providerProblem())
	}
}

func TestPullRequestAndUsageAreMirroredOnce(t *testing.T) {
	h := newHarness(t)
	task := h.readyTask("Open a pull request")
	h.cycle()
	_, run := h.fake.onlyRun()
	h.fake.update(func() {
		run.Run.Status.Phase = "Running"
		run.Run.Status.Usage = &Usage{Tokens: 1200, ReservedTokens: 300, CostMinor: 4, Currency: "EUR"}
	})
	h.cycle()
	h.cycle()
	usage := func() []domain.Event {
		var found []domain.Event
		for _, event := range h.detail(task.ID).Events {
			if event.Type == "model.usage" {
				found = append(found, event)
			}
		}
		return found
	}
	if got := usage(); len(got) != 1 || got[0].Payload["reported_tokens"] != float64(1200) || got[0].Payload["reported_cost_micros"] != float64(40000) || got[0].Payload["currency"] != "EUR" {
		t.Fatalf("usage = %+v", got)
	}
	h.fake.update(func() {
		succeed(run)
		run.Run.Status.Landing = nil
		run.Run.Status.Usage.Tokens = 2000
		run.Run.Status.PullRequest = &struct {
			URL string `json:"url"`
		}{URL: "https://github.com/jmlero/marina-acceptance/pull/7"}
	})
	h.cycle()
	h.cycle()
	detail := h.detail(task.ID)
	if len(usage()) != 2 || !strings.Contains(detail.Runs[0].ResultSummary, "https://github.com/jmlero/marina-acceptance/pull/7") {
		t.Fatalf("usage %d summary %q", len(usage()), detail.Runs[0].ResultSummary)
	}
}

func TestAPlanApprovedInMarinaStartsAfterItsExpiryPassedInDeck(t *testing.T) {
	h := newHarness(t)
	task := h.readyTask("Approve while Deck is away")
	h.cycle()
	name, run := h.fake.onlyRun()
	h.fake.decide(name, "Run", name+"-plan-1", "plan-approval", "Approve plan", "Request changes")
	h.fake.update(func() {
		decision := h.fake.decisions[name+"-plan-1"]
		decision.Spec.ExpiresAt = "2001-01-01T00:00:00Z"
		decision.Spec.Response = &struct {
			Answer     string `json:"answer"`
			AnsweredBy string `json:"answeredBy"`
		}{Answer: "Approve plan"}
		decision.Status.Phase = "Answered"
		run.Run.Status.Phase = "Running"
	})
	h.cycle()
	if status := h.detail(task.ID).Task.Status; status != domain.TaskRunning {
		t.Fatalf("status = %s", status)
	}
}

func TestAnUnreadableTokenIsNamedInTheProviderStatus(t *testing.T) {
	h := newHarness(t)
	client, err := New(Config{URL: "http://127.0.0.1:1", TokenFile: filepath.Join(t.TempDir(), "missing")})
	if err != nil {
		t.Fatal(err)
	}
	h.engine.Client, h.engine.synced = client, nil
	h.cycle()
	if problem := h.providerProblem(); !strings.Contains(problem, "token") {
		t.Fatalf("problem = %q", problem)
	}
}

func TestARepeatedProblemIsLoggedOnce(t *testing.T) {
	h := newHarness(t)
	var logs strings.Builder
	h.engine.Log = slog.New(slog.NewTextHandler(&logs, nil))
	h.fake.refuseCreate = 503
	h.readyTask("Wait for Marina")
	for range 3 {
		h.cycle()
	}
	if count := strings.Count(logs.String(), "Marina create a run"); count != 1 {
		t.Fatalf("logged %d times:\n%s", count, logs.String())
	}
}

func TestAChatTurnIsGoneOnlyWhenItsProjectAnswers(t *testing.T) {
	h := newHarness(t)
	invocation, err := h.store.InvokeConversation(h.ctx, domain.ConversationInvocationInput{ChannelID: h.project.Channels[0].ID, AgentID: store.MarinaAgentID, Body: "Hello", RequestID: "r-hello", AuthorID: "operator:local"})
	if err != nil {
		t.Fatal(err)
	}
	h.cycle()
	replies := func() int {
		page, err := h.store.PageThreadMessages(h.ctx, invocation.Thread.ID, 50, "")
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Items)
	}
	h.fake.update(func() { h.fake.projectDown = true; delete(h.fake.turns, store.MarinaName(invocation.Message.ID, "")) })
	h.cycle()
	if replies() != 1 {
		t.Fatal("a missing turn with an unreachable project was ended")
	}
	h.fake.update(func() { h.fake.projectDown = false })
	h.cycle()
	if replies() != 2 {
		t.Fatal("a turn Marina no longer has was not ended")
	}
}

func TestMarinaIsConnectedAfterAContactEvenWithNoRunInTheProject(t *testing.T) {
	h := newHarness(t)
	h.cycle()
	status, err := h.store.GetProviderStatus(h.ctx, h.project.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Connectivity != "connected" || status.LastSeenAt == nil {
		t.Fatalf("connectivity = %q, last seen %v", status.Connectivity, status.LastSeenAt)
	}
}
