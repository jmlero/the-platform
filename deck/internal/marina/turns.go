package marina

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
)

const (
	maxTurnMessages  = 64
	maxTurnBodyBytes = 16 << 10
	maxReplyBytes    = 8 << 10
)

// dispatchTurns sends each chat request addressed to Marina as one Turn with
// the thread and context frozen at invocation.
func (e *Engine) dispatchTurns(ctx context.Context, mappings []ProjectMapping) {
	ids := make([]string, 0, len(mappings))
	for _, m := range mappings {
		ids = append(ids, m.DeckProject)
	}
	invocations, err := e.Store.PendingMarinaInvocations(ctx, ids)
	if err != nil {
		e.Log.Warn("list chat requests", "error", err)
		return
	}
	for _, invocation := range invocations {
		m, _ := mappingFor(mappings, invocation.ProjectID)
		spec, err := e.turnSpec(ctx, m, invocation)
		if err != nil {
			e.failTurn(ctx, invocation.ID, "", err.Error())
			continue
		}
		name := store.MarinaName(invocation.ID, "")
		if err := e.Store.BeginMarinaTurn(ctx, invocation.ID, name, m.Namespace, spec); err != nil {
			e.problem(invocation.ProjectID, "submit a chat turn", err)
			continue
		}
		e.createTurn(ctx, store.MarinaTurn{Name: name, InvocationMessageID: invocation.ID, ProjectID: invocation.ProjectID, Namespace: m.Namespace, Spec: spec})
	}
}

func (e *Engine) turnSpec(ctx context.Context, m ProjectMapping, invocation domain.ChannelMessage) (json.RawMessage, error) {
	thread, err := e.Store.ThreadUpTo(ctx, invocation.ID, maxTurnMessages)
	if err != nil {
		return nil, err
	}
	messages := []TurnMessage{}
	for _, message := range thread {
		role := map[string]string{"human": "human", "agent": "agent"}[message.AuthorType]
		if role == "" {
			continue // Deck's own system notes are not part of the conversation.
		}
		messages = append(messages, TurnMessage{ID: message.ID, Role: role, Body: bounded(message.Body, maxTurnBodyBytes)})
	}
	resolved, err := e.Store.ResolveConversationContext(ctx, invocation.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	items, err := contextItems(resolved)
	if err != nil {
		return nil, err
	}
	budget, ok := e.turnBudgets[m.DeckProject]
	if !ok {
		budget = TurnBudget{TimeMinutes: 15, MaxTokens: 40000}
	}
	return json.Marshal(TurnSpec{Project: m.Project, Workload: m.ConversationWorkload, Thread: TurnThread{ID: invocation.ThreadID, Messages: messages}, Context: items, Budget: budget})
}

func (e *Engine) createTurn(ctx context.Context, turn store.MarinaTurn) bool {
	_, err := e.Client.CreateTurn(ctx, turn.Namespace, turn.Name, turn.Spec)
	if err == nil {
		return e.Store.MarkMarinaTurnCreated(ctx, turn.Name) == nil
	}
	if !IsRejected(err) {
		e.problem(turn.ProjectID, "create a chat turn", err)
		return false
	}
	e.Log.Warn("Marina rejected a chat turn", "turn", turn.Name, "error", err)
	e.failTurn(ctx, turn.InvocationMessageID, turn.Name, "Marina rejected the chat turn: "+rejection(err))
	return false
}

// failTurn posts a visible reply instead of leaving the request waiting.
func (e *Engine) failTurn(ctx context.Context, invocationID, name, reason string) {
	body := bounded(reason, maxReplyBytes)
	_, err := e.Store.PublishConversationOutcome(ctx, domain.ConversationOutcomeInput{InvocationMessageID: invocationID, AgentID: store.MarinaAgentID,
		Status: "blocked", Body: body, ResultDigest: Digest(body)})
	if err != nil {
		e.Log.Warn("record a failed chat turn", "invocation", invocationID, "error", err)
		return
	}
	if name != "" {
		_ = e.Store.FinishMarinaTurn(ctx, name)
	}
}

func (e *Engine) mirrorTurns(ctx context.Context) {
	turns, err := e.Store.ActiveMarinaTurns(ctx)
	if err != nil {
		e.Log.Warn("list Marina turns", "error", err)
		return
	}
	for _, turn := range turns {
		if !turn.Created && !e.createTurn(ctx, turn) {
			continue
		}
		if err := e.mirrorTurn(ctx, turn); err != nil {
			e.problem(turn.ProjectID, "mirror chat turn "+turn.Name, err)
		}
	}
}

func (e *Engine) mirrorTurn(ctx context.Context, turn store.MarinaTurn) error {
	remote, err := e.Client.Turn(ctx, turn.Namespace, turn.Name)
	if err != nil {
		if gone, err := e.gone(ctx, err, turn.Namespace, turn.Spec); !gone {
			return err
		}
		e.failTurn(ctx, turn.InvocationMessageID, turn.Name, "The Marina chat turn no longer exists.")
		return nil
	}
	for _, name := range remote.Status.Decisions {
		decision, err := e.Client.Decision(ctx, turn.Namespace, name)
		if IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if decision.Spec.Kind != "clarification" {
			continue
		}
		var expires int64
		if decision.Spec.ExpiresAt != "" {
			expires = parseTime(decision.Spec.ExpiresAt)
		}
		question, err := e.Store.EnsureConversationQuestion(ctx, domain.ConversationQuestionInput{InvocationMessageID: turn.InvocationMessageID, AgentID: store.MarinaAgentID,
			QuestionID: name, Text: bounded(decision.Spec.Question, 2000), Choices: decision.Spec.Options, ExpiresAt: expires})
		if err != nil {
			return err
		}
		if question.Status == "pending" && decision.Spec.Response != nil {
			// Answered outside Deck; show the answer in the thread.
			if _, err := e.Store.AnswerConversationQuestion(ctx, question.ID, decision.Spec.Response.Answer, store.MarinaAgentID, "marina-"+name); err != nil {
				return err
			}
		}
	}
	switch remote.Status.Phase {
	case "Succeeded":
		return e.publishTurn(ctx, turn, remote)
	case "Failed", "Cancelled":
		reason := remote.Status.Reason
		if reason == "" {
			reason = "Marina ended the chat turn as " + remote.Status.Phase + "."
		}
		e.failTurn(ctx, turn.InvocationMessageID, turn.Name, "Marina could not finish this turn: "+reason)
	}
	return nil
}

// publishTurn records the reply and any proposal through Deck's own rules. A
// proposal Deck cannot accept is dropped with a visible note.
func (e *Engine) publishTurn(ctx context.Context, turn store.MarinaTurn, remote Turn) error {
	body := ""
	if remote.Status.Reply != nil {
		body = remote.Status.Reply.Answer
	}
	if body == "" {
		body = "Marina finished without a reply."
	}
	encoded, _ := json.Marshal(remote.Status)
	in := domain.ConversationOutcomeInput{InvocationMessageID: turn.InvocationMessageID, AgentID: store.MarinaAgentID, Status: "completed",
		Body: bounded(body, maxReplyBytes), ResultDigest: Digest(string(encoded))}
	if p := remote.Status.Proposals; p != nil {
		switch {
		case p.PRDTasks != nil:
			proposal := domain.PRDTaskProposal{PublicationTaskID: p.PRDTasks.Source}
			if publication, err := e.Store.GetTask(ctx, p.PRDTasks.Source); err == nil && publication.Landing != nil {
				proposal.Revision = publication.Landing.Commit
			}
			for _, task := range p.PRDTasks.Tasks {
				proposal.Tasks = append(proposal.Tasks, domain.PRDTaskDraft{Title: task.Title, Description: task.Description, AcceptanceCriteria: task.AcceptanceCriteria})
			}
			in.PRDTasks = &proposal
		case p.PRD != nil:
			in.PRD = &domain.PRDDraft{Title: p.PRD.Title, Path: p.PRD.Path, Content: p.PRD.Content}
		case p.Task != nil:
			in.Task = &domain.CreateTaskInput{Title: p.Task.Title, Description: p.Task.Description, AcceptanceCriteria: p.Task.AcceptanceCriteria, Status: domain.TaskBacklog}
		}
	}
	_, err := e.Store.PublishConversationOutcome(ctx, in)
	if err != nil && (in.Task != nil || in.PRD != nil || in.PRDTasks != nil) && (errors.Is(err, domain.ErrInvalidInput) || errors.Is(err, domain.ErrConflict)) {
		e.Log.Warn("Marina proposal rejected by Deck", "turn", turn.Name, "error", err)
		in.Task, in.PRD, in.PRDTasks = nil, nil, nil
		in.Body = bounded(fmt.Sprintf("%s\n\n(Deck could not accept the proposed work: %v)", body, err), maxReplyBytes)
		_, err = e.Store.PublishConversationOutcome(ctx, in)
	}
	if err != nil {
		return err
	}
	return e.Store.FinishMarinaTurn(ctx, turn.Name)
}
