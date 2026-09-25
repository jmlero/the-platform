package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/conversationbyo"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"google.golang.org/protobuf/encoding/protojson"
)

type Profile struct {
	Engine        rt.EngineSelection              `json:"engine"`
	BudgetMinutes int                             `json:"budget_minutes"`
	MaxTokens     int64                           `json:"max_tokens"`
	Delegation    *workcontrol.ConversationBudget `json:"delegation,omitempty"`
}

func (p Profile) Validate(model workerapi.Model) error {
	if err := p.Engine.Validate(); err != nil {
		return err
	}
	if p.Engine.Version != kagent.Version || p.Engine.Endpoint != "kint-engine-controller.kint-engine.svc:8083" ||
		p.Engine.Namespace != "kint-engine" || p.Engine.Harness != "kint-conversation-byo" ||
		p.BudgetMinutes < 1 || p.BudgetMinutes > 30 || p.MaxTokens < 4096 || p.MaxTokens > 100000 ||
		model.MaxOutputTokens < 1 || model.MaxOutputTokens > p.MaxTokens-2048 {
		return errors.New("conversation requires the pinned read-only kagent BYO profile and bounded model budget")
	}
	if p.Delegation != nil {
		b := p.Delegation
		if b.TimeMinutes < 1 || b.MaxAttempts != 2 || b.MaxTokens < 1 || b.MaxConcurrency != 1 || b.MaxCostMinor < 0 || len(b.Authority) != 3 ||
			!slices.Contains(b.Authority, "source.read") || !slices.Contains(b.Authority, "source.write") || !slices.Contains(b.Authority, "pull_request.create") {
			return errors.New("conversation delegation requires a fixed bounded repository grant")
		}
	}
	return nil
}

func (p Profile) Digest(model workerapi.Model) string {
	encoded, _ := json.Marshal(struct {
		Profile Profile
		Model   workerapi.Model
	}{p, model})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

type Controller struct {
	ProjectID, AgentID, PolicyVersion string
	DelegationProfileDigest           string
	Profile                           Profile
	Model                             workerapi.Model
	Source                            workcontrol.ConversationSource
	Journal                           *sqlite.Store
	Blobs                             *artifacts.Store
	Engine                            rt.Adapter
	Gateway                           *Gateway
	InstallCredential                 func(context.Context, rt.Specification, string) error
}

func (c *Controller) Cycle(ctx context.Context) []error {
	var problems []error
	if err := c.Profile.Validate(c.Model); err != nil {
		return []error{err}
	}
	if err := c.scanInvocations(ctx); err != nil {
		problems = append(problems, err)
	}
	turns, err := c.Journal.ListActiveConversationTurns(ctx, c.ProjectID, c.AgentID, 1)
	if err != nil {
		return append(problems, err)
	}
	for _, turn := range turns {
		if err := c.advance(ctx, turn); err != nil {
			problems = append(problems, fmt.Errorf("conversation %s: %w", turn.MessageID, err))
		}
	}
	finished, err := c.Journal.ListUnpublishedConversationTurns(ctx, c.ProjectID, c.AgentID, 20)
	if err != nil {
		return append(problems, err)
	}
	for _, turn := range finished {
		if err := c.publishOutcome(ctx, turn); err != nil {
			problems = append(problems, fmt.Errorf("publish conversation %s: %w", turn.MessageID, err))
		}
	}
	return problems
}

func (c *Controller) scanInvocations(ctx context.Context) error {
	cursor, err := c.Journal.GetConversationDispatchCursor(ctx, c.ProjectID)
	if err != nil {
		return err
	}
	items, err := c.Source.ListConversationInvocations(ctx, c.ProjectID, cursor)
	if err != nil {
		return err
	}
	for _, invocation := range items {
		message := invocation.Message
		if invocation.EventID <= cursor || message.ChannelID == "" || message.ProjectID != c.ProjectID {
			return errors.New("conversation invocation feed escaped project or order")
		}
		if message.InvokedAgentID == c.AgentID {
			if invocation.InvokedAt < message.CreatedAt || invocation.InvokedAt > time.Now().Add(time.Minute).UnixMilli() {
				return errors.New("conversation invocation has invalid creation time")
			}
			deadline := time.UnixMilli(invocation.InvokedAt).UTC().Add(time.Duration(c.Profile.BudgetMinutes) * time.Minute)
			if _, err := c.Journal.EnsureConversationTurn(ctx, message, deadline, c.Profile.MaxTokens); err != nil {
				return err
			}
		}
		if err := c.Journal.SetConversationDispatchCursor(ctx, c.ProjectID, invocation.EventID); err != nil {
			return err
		}
		cursor = invocation.EventID
	}
	return nil
}

func (c *Controller) input(ctx context.Context, turn sqlite.ConversationTurn) (conversationbyo.Input, error) {
	in := conversationbyo.Input{Version: 1, ProjectID: turn.ProjectID, ChannelID: turn.ChannelID, ThreadID: turn.ThreadID, MessageID: turn.MessageID}
	cursor := ""
	found := false
	for pageIndex := 0; pageIndex < 10 && !found; pageIndex++ {
		page, err := c.Source.PageConversationMessages(ctx, turn.ChannelID, cursor)
		if err != nil {
			return in, err
		}
		for _, m := range page.Items {
			if m.ProjectID != turn.ProjectID || m.ChannelID != turn.ChannelID {
				return in, errors.New("conversation history escaped channel scope")
			}
			if m.ThreadID == turn.ThreadID && (m.AuthorType == "human" || m.AuthorType == "agent") && m.Body != "" {
				in.Messages = append(in.Messages, conversationbyo.Message{ID: m.ID, Role: m.AuthorType, Body: m.Body, TaskIDs: m.References.TaskIDs})
				if len(in.Messages) > 20 {
					in.Messages = in.Messages[1:]
				}
			}
			if m.ID == turn.MessageID {
				if m.Body != turn.Body || m.InvokedAgentID != turn.AgentID || m.InvocationRequestID != turn.RequestID {
					return in, errors.New("invoked message changed after recording")
				}
				found = true
				break
			}
		}
		if found || page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if !found {
		return in, errors.New("invoked message not found in bounded channel history")
	}
	for len(in.Messages) > 1 {
		total := 0
		for _, m := range in.Messages {
			total += len(m.Body)
		}
		if total <= 32<<10 {
			break
		}
		in.Messages = in.Messages[1:]
	}
	in.InputDigest = conversationbyo.DigestInput(in)
	if err := conversationbyo.ValidateInput(in); err != nil {
		return in, err
	}
	return in, nil
}

func (c *Controller) specification(ctx context.Context, turn sqlite.ConversationTurn) (rt.Specification, error) {
	in, err := c.input(ctx, turn)
	if err != nil {
		return rt.Specification{}, err
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		return rt.Specification{}, err
	}
	digest, err := c.Blobs.Put(encoded)
	if err != nil {
		return rt.Specification{}, err
	}
	selection := c.Profile.Engine
	spec := rt.Specification{Engine: &selection, Workload: "conversation/v1", InputDigest: in.InputDigest,
		ProfileDigest: c.Profile.Digest(c.Model), ExecutionID: turn.MessageID, AttemptID: turn.MessageID,
		IdempotencyKey: "conversation-" + turn.MessageID, ContextURI: "artifact://" + digest, ContextDigest: digest,
		HarnessVersion: "conversation-byo/v1", Limits: rt.Limits{CPUMillis: 500, MemoryBytes: 512 << 20, EphemeralStorageBytes: 512 << 20,
			WallTimeSeconds: int64(c.Profile.BudgetMinutes * 60), MaxProcesses: 32},
		NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral", AuthorityFence: 1, DeadlineAt: turn.DeadlineAt,
		PolicyVersion: c.PolicyVersion, CredentialReferences: []rt.CredentialReference{{Kind: "engine-gateway",
			Reference: "secret://" + selection.Namespace + "/" + selection.Template + "/gateway", ExpiresAt: turn.DeadlineAt}}}
	if err := spec.Validate(time.Now()); err != nil {
		return rt.Specification{}, err
	}
	return spec, nil
}

func (c *Controller) advance(ctx context.Context, turn sqlite.ConversationTurn) error {
	if turn.Status == "pending" {
		if !time.Now().Before(turn.DeadlineAt) {
			return c.Journal.FinishConversationTurn(ctx, turn.MessageID, "blocked", "", "Conversation deadline expired before dispatch")
		}
		owner, err := c.Journal.ConversationTemplateOwner(ctx, c.Profile.Engine.Namespace, c.Profile.Engine.Template)
		if err != nil {
			return err
		}
		if owner != "" && owner != turn.MessageID {
			return c.Journal.FinishConversationTurn(ctx, turn.MessageID, "blocked", "", "Prepared conversation template was already used by another turn")
		}
		spec, err := c.specification(ctx, turn)
		if err != nil {
			return err
		}
		if err := c.Journal.SaveConversationSpecification(ctx, turn.MessageID, spec); err != nil {
			return err
		}
		turn, err = c.Journal.GetConversationTurn(ctx, turn.MessageID)
		if err != nil {
			return err
		}
	}
	if turn.SpecificationDigest == "" {
		return errors.New("conversation lacks frozen engine specification")
	}
	spec := turn.Specification
	if spec.ProfileDigest != c.Profile.Digest(c.Model) || spec.PolicyVersion != c.PolicyVersion {
		return c.stop(ctx, turn, "Conversation profile changed before completion")
	}
	if !time.Now().Before(turn.DeadlineAt) {
		return c.stop(ctx, turn, "Conversation deadline exceeded")
	}
	engineRecord, err := c.Journal.ConversationEngineJournal().GetEngineAttempt(ctx, turn.MessageID)
	if err == nil && engineRecord.State.Terminal() {
		return c.finish(ctx, turn, rt.Observation{Found: true, ProviderHandle: "kagent:" + turn.MessageID, State: engineRecord.State})
	}
	credential, err := c.Gateway.EngineCredential(ctx, spec)
	if err != nil {
		return err
	}
	if c.InstallCredential == nil {
		return errors.New("conversation engine credential installer unavailable")
	}
	if err := c.InstallCredential(ctx, spec, credential); err != nil {
		return err
	}
	if _, err := c.Journal.ConversationEngineJournal().EnsureEngineAttempt(ctx, spec); err != nil {
		return err
	}
	obs, err := c.Engine.Start(ctx, spec)
	if errors.Is(err, kagent.ErrInteractionRequired) && obs.Question != nil {
		return c.question(ctx, turn, obs)
	}
	if errors.Is(err, kagent.ErrInteractionRequired) && obs.Question == nil {
		current, journalErr := c.Journal.ConversationEngineJournal().GetEngineAttempt(ctx, turn.MessageID)
		if journalErr != nil {
			return journalErr
		}
		if current.AnswerPhase == "sending" || current.AnswerPhase == "sent" {
			return nil
		}
	}
	if err != nil {
		return err
	}
	if obs.Question != nil {
		return c.question(ctx, turn, obs)
	}
	if obs.State.Terminal() {
		return c.finish(ctx, turn, obs)
	}
	return nil
}

func (c *Controller) question(ctx context.Context, turn sqlite.ConversationTurn, obs rt.Observation) error {
	if obs.Question == nil || obs.Question.ID == "" || strings.TrimSpace(obs.Question.Text) == "" {
		return errors.New("conversation engine question missing identity")
	}
	q, err := c.Source.EnsureConversationQuestion(ctx, workcontrol.ConversationQuestionInput{
		InvocationMessageID: turn.MessageID, AgentID: turn.AgentID, QuestionID: obs.Question.ID,
		Text: obs.Question.Text, Choices: obs.Question.Choices,
	})
	if err != nil {
		return err
	}
	if q.ProjectID != turn.ProjectID || q.ChannelID != turn.ChannelID || q.ThreadID != turn.ThreadID ||
		q.InvocationMessageID != turn.MessageID || q.AgentID != turn.AgentID || q.QuestionID != obs.Question.ID {
		return errors.New("conversation question escaped originating invocation")
	}
	if q.Status == "pending" {
		return nil
	}
	if q.Status != "answered" || q.AnswerMessageID == "" || strings.TrimSpace(q.Answer) == "" || len(q.Answer) > 2000 {
		return errors.New("conversation question answer is invalid")
	}
	answerer, ok := c.Engine.(rt.QuestionAnswerer)
	if !ok {
		return errors.New("conversation engine cannot resume a question")
	}
	next, err := answerer.AnswerQuestion(ctx, obs.ProviderHandle, obs.Question.ID, q.Answer)
	if err != nil {
		return err
	}
	if next.State.Terminal() {
		return c.finish(ctx, turn, next)
	}
	return nil
}

func (c *Controller) stop(ctx context.Context, turn sqlite.ConversationTurn, reason string) error {
	if turn.SpecificationDigest != "" {
		if _, err := c.Journal.ConversationEngineJournal().GetEngineAttempt(ctx, turn.MessageID); err == nil {
			obs, err := c.Engine.Cancel(ctx, "kagent:"+turn.MessageID, true)
			if err != nil {
				return err
			}
			if obs.State != rt.StateCancelled && !obs.State.Terminal() {
				return errors.New("conversation termination has not been observed")
			}
			if _, err := c.Engine.CollectArtifacts(ctx, "kagent:"+turn.MessageID); err != nil {
				return err
			}
			if err := c.Engine.Cleanup(ctx, "kagent:"+turn.MessageID); err != nil {
				return err
			}
		} else if !errors.Is(err, execution.ErrNotFound) {
			return err
		}
	}
	return c.Journal.FinishConversationTurn(ctx, turn.MessageID, "blocked", "", reason)
}

func (c *Controller) finish(ctx context.Context, turn sqlite.ConversationTurn, obs rt.Observation) error {
	if obs.State != rt.StateSucceeded {
		return c.stop(ctx, turn, "Conversation agent failed or was cancelled")
	}
	items, err := c.Engine.CollectArtifacts(ctx, "kagent:"+turn.MessageID)
	if err != nil {
		return err
	}
	var selected *conversationbyo.Result
	for _, item := range items {
		if item.Kind != "kagent-a2a-output" || item.RuntimeAttemptID != turn.MessageID {
			continue
		}
		encoded, err := c.Blobs.Get(item.Digest)
		if err != nil || len(encoded) > 64<<10 || int64(len(encoded)) != item.SizeBytes {
			return errors.New("conversation engine artifact is invalid")
		}
		var envelope a2a.Artifact
		if protojson.Unmarshal(encoded, &envelope) != nil {
			return errors.New("conversation engine artifact envelope is invalid")
		}
		if envelope.Name != "marina-conversation-result.json" {
			continue
		}
		if selected != nil || len(envelope.Parts) != 1 || envelope.Parts[0].MediaType != "application/json" {
			return errors.New("conversation result is ambiguous")
		}
		raw, ok := envelope.Parts[0].Content.(*a2a.Part_Raw)
		if !ok || len(raw.Raw) > 16<<10 {
			return errors.New("conversation result must be inline JSON")
		}
		var result conversationbyo.Result
		decoder := json.NewDecoder(strings.NewReader(string(raw.Raw)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || conversationbyo.ValidateResult(result) != nil || result.MessageID != turn.MessageID ||
			result.ThreadID != turn.ThreadID || result.InputDigest != turn.Specification.InputDigest {
			return errors.New("conversation result identity or content is invalid")
		}
		selected = &result
	}
	if selected == nil {
		return errors.New("completed conversation has no selected result")
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		return err
	}
	digest, err := c.Blobs.Put(encoded)
	if err != nil {
		return err
	}
	if err := c.Engine.Cleanup(ctx, "kagent:"+turn.MessageID); err != nil {
		return err
	}
	return c.Journal.FinishConversationTurn(ctx, turn.MessageID, "completed", digest, "")
}

func (c *Controller) publishOutcome(ctx context.Context, turn sqlite.ConversationTurn) error {
	in := workcontrol.ConversationOutcomeInput{InvocationMessageID: turn.MessageID, AgentID: turn.AgentID, Status: turn.Status, ResultDigest: turn.ResultDigest}
	if turn.PublicationJSON != "" {
		if err := json.Unmarshal([]byte(turn.PublicationJSON), &in); err != nil || in.InvocationMessageID != turn.MessageID || in.AgentID != turn.AgentID || in.Status != turn.Status {
			return errors.New("recorded conversation publication is invalid")
		}
	} else if turn.Status == "completed" {
		encoded, err := c.Blobs.Get(turn.ResultDigest)
		if err != nil {
			return err
		}
		var result conversationbyo.Result
		decoder := json.NewDecoder(strings.NewReader(string(encoded)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || conversationbyo.ValidateResult(result) != nil ||
			result.MessageID != turn.MessageID || result.ThreadID != turn.ThreadID || result.InputDigest != turn.Specification.InputDigest {
			return errors.New("recorded conversation result changed before publication")
		}
		in.Body = result.Answer
		if d := result.Delegation; d != nil {
			status := "backlog"
			var budget *workcontrol.ConversationBudget
			if d.Mode == "ready" && c.Profile.Delegation != nil && c.DelegationProfileDigest != "" && turn.Specification.ProfileDigest == c.Profile.Digest(c.Model) {
				status = "ready"
				copy := *c.Profile.Delegation
				budget = &copy
				in.ProfileID, in.ProfileDigest = "repository/v1", c.DelegationProfileDigest
			}
			in.Task = &workcontrol.ConversationTaskInput{Title: d.Title, Description: d.Description, AcceptanceCriteria: d.AcceptanceCriteria, Status: status, Budget: budget}
			in.FollowupTaskID = d.FollowupTaskID
			if status == "ready" {
				in.Body += "\n\nTask authorized: " + d.Title
			} else {
				in.Body += "\n\nBacklog draft created: " + d.Title
			}
		}
	} else if turn.Status == "blocked" {
		in.Body = "Conversation blocked: " + turn.Reason
		hash := sha256.Sum256([]byte(turn.SpecificationDigest + ":" + turn.Reason))
		in.ResultDigest = hex.EncodeToString(hash[:])
	} else {
		return errors.New("conversation is not terminal")
	}
	if len(in.Body) > 8<<10 {
		return errors.New("conversation publication exceeds reply limit")
	}
	if err := c.Journal.SaveConversationPublication(ctx, turn.MessageID, in); err != nil {
		return err
	}
	out, err := c.Source.PublishConversationOutcome(ctx, in)
	if err != nil {
		return err
	}
	if out.InvocationMessageID != in.InvocationMessageID || out.Status != in.Status || out.ResultDigest != in.ResultDigest || out.MessageID == "" || (in.Task == nil && out.TaskID != "") || (in.Task != nil && out.TaskID == "") || out.FollowupTaskID != in.FollowupTaskID {
		return errors.New("conversation publication escaped source identity")
	}
	return c.Journal.MarkConversationPublished(ctx, turn.MessageID, out.MessageID, out.TaskID)
}
