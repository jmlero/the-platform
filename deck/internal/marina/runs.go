package marina

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
)

const maxContextItems = 32

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Marina events that Deck already records as its own run transitions.
var transitionEvents = map[string]bool{
	"run.accepted": true, "run.claimed": true, "plan.submitted": true, "decision.requested": true, "decision.answered": true,
	"run.started": true, "run.verifying": true, "evidence.recorded": true, "run.completed": true, "run.blocked": true,
	"run.failed": true, "run.cancel_requested": true, "run.cancelled": true,
}

// Marina evidence kinds and the Deck evidence types landing checks require.
var evidenceTypes = map[string]string{"review": "semantic_review", "verification": "test", "ci": "ci", "landing": "landing", "publication": "publication"}

func (e *Engine) dispatchRuns(ctx context.Context, mappings []ProjectMapping) {
	ids := make([]string, 0, len(mappings))
	for _, m := range mappings {
		ids = append(ids, m.DeckProject)
	}
	tasks, err := e.Store.MarinaReadyTasks(ctx, ids)
	if err != nil {
		e.Log.Warn("list ready work", "error", err)
		return
	}
	for _, task := range tasks {
		m, _ := mappingFor(mappings, task.ProjectID)
		profile := profileFor(task)
		workload := m.Workloads[profile]
		// Submit only to a Workload whose delivery matches the task's authority.
		if workload == "" || !e.profiles[task.ProjectID][profile] {
			continue
		}
		spec, specErr := e.runSpec(ctx, m, workload, task)
		run, err := e.Store.BeginMarinaRun(ctx, task.ID, task.Version, m.Namespace, spec)
		if err != nil {
			if !errors.Is(err, domain.ErrConflict) {
				e.problem(task.ProjectID, "submit work", err)
			}
			continue
		}
		if specErr != nil {
			// Nothing is sent; the task shows why it cannot run.
			if err := e.Store.MarkMarinaRunCreated(ctx, run.ID); err != nil {
				e.problem(task.ProjectID, "submit work", err)
			} else if err := e.Store.BlockRun(ctx, mutation(run, "block"), run.ID, specErr.Error()); err != nil {
				e.problem(task.ProjectID, "submit work", err)
			}
			continue
		}
		e.createRun(ctx, store.MarinaRun{Run: run, ProjectID: task.ProjectID, Namespace: m.Namespace, Spec: spec})
	}
}

// runSpec freezes the task as a Run spec. The bytes are stored before the
// first create, so every retry is an exact replay.
func (e *Engine) runSpec(ctx context.Context, m ProjectMapping, workload string, task domain.Task) (json.RawMessage, error) {
	resolved, err := e.Store.ResolveTaskContext(ctx, task.ID)
	if err != nil {
		return nil, fmt.Errorf("the task's frozen context is unavailable: %w", err)
	}
	items, err := contextItems(resolved)
	if err != nil {
		return nil, err
	}
	spec := RunSpec{Project: m.Project, Workload: workload, Title: task.Title, Description: task.Description, AcceptanceCriteria: task.AcceptanceCriteria,
		Budget: Budget{TimeMinutes: task.Budget.TimeMinutes, MaxTokens: task.Budget.MaxTokens, MaxAttempts: task.Budget.MaxAttempts}, Context: items}
	if task.Budget.MaxCostMinor > 0 {
		spec.Budget.MaxCostMinor, spec.Budget.Currency = task.Budget.MaxCostMinor, task.Budget.Currency
	}
	if task.PRD != nil {
		spec.Publish = &Publish{Path: task.PRD.Path, Content: task.PRD.Content, Digest: Digest(task.PRD.Content)}
	}
	return json.Marshal(spec)
}

// contextItems carries the frozen sources and messages with content digests.
// Revoked sources are withheld.
func contextItems(resolved domain.ResolvedContext) ([]ContextItem, error) {
	items := []ContextItem{}
	for _, source := range resolved.Sources {
		if source.RevokedAt != 0 {
			continue
		}
		item := ContextItem{Name: bounded(source.Title, 200), Kind: "document", Trust: source.Trust, Content: source.Content, Digest: Digest(source.Content),
			Source: &ContextSource{System: "deck", ID: source.ID, Revision: source.Revision}}
		if source.Kind == "git" && source.Path != "" && revisionPattern.MatchString(source.SourceRevision) {
			item.Kind, item.Content, item.Path, item.Revision = "repository-file", "", source.Path, source.SourceRevision
			item.Digest = Digest(source.Path + "@" + source.SourceRevision)
		}
		items = append(items, item)
	}
	for _, message := range resolved.Messages {
		items = append(items, ContextItem{Name: bounded("Message "+message.ID, 200), Kind: "message", Trust: "data", Content: message.Content, Digest: Digest(message.Content)})
	}
	if len(items) > maxContextItems {
		return nil, fmt.Errorf("the frozen context has %d items; Marina accepts at most %d", len(items), maxContextItems)
	}
	return items, nil
}

func bounded(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8Start(value[limit]) {
		limit--
	}
	return value[:limit]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// createRun sends the recorded spec until Marina confirms it. A request Marina
// refuses for good blocks the task with Marina's reason.
func (e *Engine) createRun(ctx context.Context, item store.MarinaRun) bool {
	_, err := e.Client.CreateRun(ctx, item.Namespace, item.Run.ID, item.Spec)
	if err == nil {
		return e.Store.MarkMarinaRunCreated(ctx, item.Run.ID) == nil
	}
	if !IsRejected(err) {
		e.problem(item.ProjectID, "create a run", err)
		return false
	}
	e.Log.Warn("Marina rejected a run", "run", item.Run.ID, "error", err)
	if e.Store.MarkMarinaRunCreated(ctx, item.Run.ID) == nil {
		_ = e.Store.BlockRun(ctx, mutation(item.Run, "block"), item.Run.ID, "Marina rejected the run: "+rejection(err))
	}
	return false
}

func rejection(err error) string {
	var status *APIError
	if errors.As(err, &status) && strings.TrimSpace(status.Message) != "" {
		return bounded(status.Message, 500)
	}
	return "invalid request"
}

func (e *Engine) mirrorRuns(ctx context.Context) {
	items, err := e.Store.ActiveMarinaRuns(ctx)
	if err != nil {
		e.Log.Warn("list Marina runs", "error", err)
		return
	}
	for _, item := range items {
		if !item.Created && !e.createRun(ctx, item) {
			continue
		}
		if err := e.mirrorRun(ctx, item); err != nil {
			e.problem(item.ProjectID, "mirror run "+item.Run.ID, err)
		}
	}
}

func (e *Engine) mirrorRun(ctx context.Context, item store.MarinaRun) error {
	remote, err := e.Client.Run(ctx, item.Namespace, item.Run.ID)
	if IsNotFound(err) {
		return e.Store.FailRun(ctx, mutation(item.Run, "gone"), item.Run.ID, "The Marina run no longer exists")
	}
	if err != nil {
		return err
	}
	if err := e.mirrorEvents(ctx, item); err != nil {
		return err
	}
	decisions := map[string]Decision{}
	var order []string
	for _, name := range remote.Status.Decisions {
		decision, err := e.Client.Decision(ctx, item.Namespace, name)
		if IsNotFound(err) {
			continue // Marina publishes the resource shortly.
		}
		if err != nil {
			return err
		}
		decisions[name] = decision
		order = append(order, name)
	}
	for range maxStepsPerCycle {
		local, err := e.Store.GetRun(ctx, item.Run.ID)
		if err != nil {
			return err
		}
		progressed, err := e.step(ctx, item, local, remote, decisions, order)
		if err != nil || !progressed {
			return err
		}
	}
	return nil
}

// mirrorEvents copies Marina's execution events, such as progress, after the
// last mirrored sequence. Lifecycle events are recorded by the transitions.
func (e *Engine) mirrorEvents(ctx context.Context, item store.MarinaRun) error {
	after := item.LastSequence
	for range 5 {
		page, err := e.Client.RunEvents(ctx, item.Namespace, item.Run.ID, after)
		if err != nil || len(page.Items) == 0 {
			return err
		}
		for _, event := range page.Items {
			if event.Sequence <= after {
				continue
			}
			if !transitionEvents[event.Type] {
				message := event.Message
				if strings.TrimSpace(message) == "" {
					message = event.Type
				}
				projected := domain.ProjectedEvent{Producer: store.MarinaAgentID, ProducerEventID: item.Run.ID + ":" + strconv.FormatInt(event.Sequence, 10),
					ExecutionID: item.Run.ID, Sequence: event.Sequence, Type: bounded(event.Type, 64), Message: bounded(message, 2000), Payload: event.Fields, OccurredAt: parseTime(event.Time)}
				key := store.Mutation{ActorID: store.MarinaAgentID, IdempotencyKey: "event:" + projected.ProducerEventID}
				if err := e.Store.AppendProjectedEvent(ctx, key, item.Run.ID, projected); err != nil {
					return err
				}
			}
			if err := e.Store.AdvanceMarinaRunSequence(ctx, item.Run.ID, event.Sequence); err != nil {
				return err
			}
			after = event.Sequence
		}
	}
	return nil
}

func active(status string) bool {
	return status == domain.RunPlanning || status == domain.RunWaitingDecision || status == domain.RunRunning || status == domain.RunVerifying
}

func pastPlanning(phase string) bool {
	return phase == "Running" || phase == "Verifying" || phase == "Succeeded"
}

// step applies the next Deck transition that Marina's state already shows.
// It reports whether it changed the ledger.
func (e *Engine) step(ctx context.Context, item store.MarinaRun, local domain.Run, remote Run, decisions map[string]Decision, order []string) (bool, error) {
	if !active(local.Status) {
		return false, nil
	}
	phase, reason := remote.Status.Phase, strings.TrimSpace(remote.Status.Reason)
	switch phase {
	case "Cancelled":
		return true, e.cancelLocally(ctx, local)
	case "Failed":
		if reason == "" {
			reason = "Marina reported that the run failed"
		}
		return true, e.Store.FailRun(ctx, mutation(local, "fail"), local.ID, reason)
	case "Blocked":
		if reason == "" {
			reason = "Marina blocked the run"
		}
		return true, e.Store.BlockRun(ctx, mutation(local, "block"), local.ID, reason)
	}
	var plan *Decision
	for _, name := range order {
		if decision := decisions[name]; decision.Spec.Kind == "plan-approval" {
			plan = &decision
		}
	}
	switch local.Status {
	case domain.RunPlanning:
		if plan != nil {
			if _, err := e.Store.GetDecision(ctx, plan.Metadata.Name); errors.Is(err, domain.ErrNotFound) {
				return true, e.submitPlan(ctx, local, item, plan)
			}
		}
		if len(remote.Status.Decisions) == 0 && pastPlanning(phase) {
			return true, e.submitPlan(ctx, local, item, nil)
		}
	case domain.RunWaitingDecision:
		current, err := e.Store.LatestDecisionForRun(ctx, local.ID)
		if err != nil {
			return false, err
		}
		if current.Status == "pending" {
			return e.mirrorAnswer(ctx, current, decisions[current.ID])
		}
		if current.Status == "answered" && strings.EqualFold(current.Answer, current.Recommendation) && pastPlanning(phase) {
			return true, e.Store.StartRun(ctx, mutation(local, "start:"+current.ID), local.ID)
		}
	case domain.RunRunning:
		if changed, err := e.mirrorQuestions(ctx, local, decisions, order); changed || err != nil {
			return changed, err
		}
		if phase == "Verifying" || phase == "Succeeded" {
			return true, e.Store.BeginVerification(ctx, mutation(local, "verify"), local.ID)
		}
	case domain.RunVerifying:
		if phase == "Succeeded" {
			return true, e.complete(ctx, item, local, remote)
		}
	}
	return false, nil
}

func (e *Engine) submitPlan(ctx context.Context, local domain.Run, item store.MarinaRun, plan *Decision) error {
	input := domain.PlanRevisionInput{Plan: "Marina planned this run without an approval step.", ContextDigest: Digest(string(item.Spec)), RiskClass: "medium",
		VerificationStrategy: "Marina verifies the acceptance criteria with independent evidence on the exact source revision.",
		RollbackApproach:     "Request changes or cancel the run."}
	request := domain.DecisionRequest{}
	key := "plan:auto"
	if plan != nil {
		input.Plan = "Marina's plan is waiting for approval."
		if details := strings.TrimSpace(plan.Spec.Details); details != "" {
			input.Plan = details
		}
		request = domain.DecisionRequest{ID: plan.Metadata.Name, Required: true, EvidenceSummary: bounded(plan.Spec.Details, 2000)}
		if plan.Spec.ExpiresAt != "" {
			expires := parseTime(plan.Spec.ExpiresAt)
			request.ExpiresAt = &expires
		}
		key = "plan:" + plan.Metadata.Name
	}
	_, _, err := e.Store.SubmitPlanRevision(ctx, mutation(local, key), local.ID, input, request)
	return err
}

// mirrorAnswer applies an answer given outside Deck, for example with
// platformctl, or closes a decision Marina expired.
func (e *Engine) mirrorAnswer(ctx context.Context, local domain.Decision, remote Decision) (bool, error) {
	switch {
	case remote.Spec.Response != nil:
		_, err := e.Store.AnswerDecision(ctx, local.ID, remote.Spec.Response.Answer)
		return err == nil, err
	case remote.Status.Phase == "Expired":
		return true, e.Store.CloseDecision(ctx, local.ID, "expired")
	case remote.Status.Phase == "Closed":
		return true, e.Store.CloseDecision(ctx, local.ID, "superseded")
	}
	return false, nil
}

// mirrorQuestions records clarification questions Marina asks while a run is
// executing, and answers given outside Deck.
func (e *Engine) mirrorQuestions(ctx context.Context, local domain.Run, decisions map[string]Decision, order []string) (bool, error) {
	for _, name := range order {
		remote := decisions[name]
		if remote.Spec.Kind != "clarification" || remote.Spec.Owner.Kind != "Run" {
			continue
		}
		existing, err := e.Store.FindEngineQuestion(ctx, local.ID, name)
		if errors.Is(err, domain.ErrNotFound) {
			if remote.Spec.Response != nil || remote.Status.Phase != "Open" || remote.Spec.ExpiresAt == "" {
				continue
			}
			_, err = e.Store.RequestEngineQuestion(ctx, mutation(local, "question:"+name), local.ID,
				domain.EngineQuestion{OperationID: name, Question: bounded(remote.Spec.Question, 2000), Options: remote.Spec.Options, ExpiresAt: parseTime(remote.Spec.ExpiresAt)})
			return err == nil, err
		}
		if err != nil {
			return false, err
		}
		if existing.Status == "pending" {
			if changed, err := e.mirrorAnswer(ctx, existing, remote); changed || err != nil {
				return changed, err
			}
		}
	}
	return false, nil
}

// complete records Marina's evidence and completes the run. A landing is
// accepted only when Deck's own landing checks pass; otherwise the result
// waits for operator acceptance.
func (e *Engine) complete(ctx context.Context, item store.MarinaRun, local domain.Run, remote Run) error {
	if local.ActivePlanRevisionID == nil {
		return errors.New("verifying run has no active plan revision")
	}
	var passed, landed []string
	commit := ""
	if remote.Status.Landing != nil {
		commit = remote.Status.Landing.Commit
	}
	for _, evidence := range remote.Status.Evidence {
		kind := evidenceTypes[evidence.Kind]
		if kind == "" || !revisionPattern.MatchString(evidence.SourceRevision) {
			continue
		}
		encoded, _ := json.Marshal(evidence)
		outcome := "failed"
		if evidence.Passed {
			outcome = "passed"
		}
		command := strings.TrimSpace(evidence.Summary)
		if command == "" {
			command = evidence.Kind
		}
		input := domain.EvidenceInput{ID: evidence.ID, RunID: local.ID, PlanRevisionID: *local.ActivePlanRevisionID, Type: kind, Producer: store.MarinaAgentID,
			Verifier: "marina/" + evidence.Kind, SubjectDigest: evidence.SourceRevision, SourceRevision: evidence.SourceRevision, Command: bounded(command, 500),
			Outcome: outcome, ProvenanceURI: "marina:" + item.Namespace + "/runs/" + local.ID + "#" + evidence.ID, ProvenanceDigest: Digest(string(encoded)),
			ObservedAt: parseTime(evidence.RecordedAt)}
		if digestPattern.MatchString(evidence.ArtifactDigest) {
			input.ArtifactURI, input.ArtifactDigest = "marina:"+item.Namespace+"/runs/"+local.ID+"/artifacts/"+evidence.ArtifactDigest, evidence.ArtifactDigest
		}
		if _, err := e.Store.RecordEvidence(ctx, input); err != nil {
			return err
		}
		if evidence.Passed {
			passed = append(passed, evidence.ID)
			if evidence.SourceRevision == commit {
				landed = append(landed, evidence.ID)
			}
		}
	}
	if len(passed) == 0 {
		return e.Store.BlockRun(ctx, mutation(local, "block"), local.ID, "Marina reported success without passed evidence")
	}
	summary := "Marina run " + local.ID + " succeeded"
	if commit != "" {
		task, err := e.Store.GetTask(ctx, local.TaskID)
		if err != nil {
			return err
		}
		if task.Context != nil && task.Context.Repository != nil {
			landing := domain.LandingObservation{Repository: task.Context.Repository.Repository, BaseBranch: task.Context.Repository.BaseBranch,
				Commit: commit, ObservedAt: parseTime(remote.Status.Landing.ObservedAt)}
			err = e.Store.CompleteLandedRun(ctx, mutation(local, "complete"), local.ID, summary+" and landed "+commit, landed, landing)
			if err == nil || !(errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrInvalidInput)) {
				return err
			}
			e.Log.Warn("Marina landing failed Deck's checks; the result awaits review", "run", local.ID, "error", err)
			if local, err = e.Store.GetRun(ctx, local.ID); err != nil {
				return err
			}
			summary += "; Deck could not confirm the landing, so the result needs review"
		}
	}
	return e.Store.CompleteRunWithEvidence(ctx, mutation(local, "complete-review"), local.ID, summary, passed)
}

// cancelLocally completes the operator's cancel request, or records Marina's
// cancellation when it came from another client.
func (e *Engine) cancelLocally(ctx context.Context, local domain.Run) error {
	commands, err := e.Store.ListCommands(ctx, local.ID, 0, 200)
	if err != nil {
		return err
	}
	var cancel *domain.Command
	for i := range commands {
		if commands[i].Type == domain.CommandCancel && (commands[i].Status == "pending" || commands[i].Status == "acknowledged") {
			cancel = &commands[i]
		}
	}
	if cancel == nil {
		created, err := e.Store.CreateCommand(ctx, local.ID, domain.CommandCancel, "Cancelled in Marina", store.MarinaAgentID)
		if err != nil {
			return err
		}
		cancel = &created
	}
	current, err := e.Store.GetRun(ctx, local.ID)
	if err != nil {
		return err
	}
	return e.Store.AcknowledgeCommand(ctx, mutation(current, "cancel:"+cancel.ID), current.ID, cancel.ID, "completed", "Marina cancelled the run")
}
