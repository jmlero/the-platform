package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/contextpack"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

var errContextClarification = errors.New("project context needs clarification")

func contextProblem(message string) error {
	return fmt.Errorf("%s; correct the project sources and authorize a new task: %w", message, errContextClarification)
}

// This conservative filter only catches explicit instruction overrides. It is
// not a general prompt-injection detector: trust labels and the external
// authority, filesystem, source and budget gates remain the security boundary.
func contextOverride(text string) bool {
	value := strings.ToLower(text)
	for _, marker := range []string{"ignore previous instructions", "ignore all previous", "ignore task permissions", "override system", "reveal credentials", "publish every secret"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func (c *Controller) assembleContext(now time.Time, e execution.Execution, task workcontrol.WorkItem, s state, resolved workcontrol.ResolvedContext) (contextpack.Package, error) {
	input := contextpack.BuildInput{ProjectID: e.ProjectID, ExecutionID: e.ID, PolicyVersion: c.Config.PolicyVersion, SourceRevision: s.Base, HarnessVersion: c.Config.Kubernetes.Image, ToolVersions: map[string]string{"worker_image": c.Config.Kubernetes.Image}}
	add := func(kind contextpack.Kind, uri, title, owner, revision, content string, trust contextpack.Trust, expires time.Time) {
		input.Inputs = append(input.Inputs, contextpack.Input{Kind: kind, URI: uri, Title: title, Owner: owner, Revision: revision, Content: content, Digest: artifacts.Digest([]byte(content)), Trust: trust, Scope: e.ProjectID, ObservedAt: now, ExpiresAt: expires})
	}
	taskBytes, _ := json.Marshal(struct {
		Title, Description, AcceptanceCriteria string
		Budget                                 workcontrol.Budget
	}{task.Title, task.Description, task.AcceptanceCriteria, task.Budget})
	policyBytes, _ := json.Marshal(struct {
		Version        string
		AllowedPaths   []string
		Checks         any
		Deadline       time.Time
		BindingVersion int64
	}{c.Config.PolicyVersion, c.Config.AllowedPaths, c.Config.Checks, e.DeadlineAt, e.BindingVersion})
	add(contextpack.KindTaskSnapshot, "work://"+e.ProjectID+"/"+task.ID, "Authorized outcome and acceptance criteria", "work-control", fmt.Sprint(task.Version), string(taskBytes), contextpack.TrustTrusted, e.DeadlineAt)
	add(contextpack.KindPolicy, "policy://"+c.Config.PolicyVersion, "Execution limits", "marina", c.Config.PolicyVersion, string(policyBytes), contextpack.TrustTrusted, e.DeadlineAt)
	add(contextpack.KindSourceRevision, c.Config.Git.CloneURL+"@"+s.Base, "Pinned repository", "git", s.Base, s.Base, contextpack.TrustTrusted, e.DeadlineAt)
	add(contextpack.KindTool, "worker://image", "Worker image", "marina", c.Config.Kubernetes.Image, c.Config.Kubernetes.Image, contextpack.TrustTrusted, e.DeadlineAt)
	for _, source := range resolved.Sources {
		uri := fmt.Sprintf("context://%s/%s/%d", e.ProjectID, source.ID, source.Revision)
		if source.Kind == "git" {
			uri = c.Config.Git.CloneURL + "@" + source.SourceRevision + "/" + source.Path
		}
		if source.Kind == "decision" {
			uri = "decision://" + source.Reference + "/" + source.SourceRevision
		}
		if source.RevokedAt != 0 {
			return contextpack.Package{}, contextProblem("Selected source " + source.ID + " was revoked")
		}
		for _, other := range resolved.Sources {
			if slices.Contains(source.ConflictsWith, other.ID) {
				return contextpack.Package{}, contextProblem("Sources " + source.ID + " and " + other.ID + " conflict")
			}
		}
		expires := e.DeadlineAt
		if source.ExpiresAt != 0 && time.UnixMilli(source.ExpiresAt).Before(expires) {
			expires = time.UnixMilli(source.ExpiresAt).UTC()
		}
		reason := ""
		if !expires.After(now) {
			reason = "Source is stale"
		}
		if contextOverride(source.Content) {
			reason = "Source contains an instruction override"
		}
		if reason != "" {
			if source.Trust == "guidance" {
				return contextpack.Package{}, contextProblem(source.ID + ": " + reason)
			}
			input.Omissions = append(input.Omissions, contextpack.Omission{URI: uri, Title: source.Title, Digest: source.Digest, Reason: reason})
			continue
		}
		trust, kind, owner := contextpack.TrustUntrusted, contextpack.KindExternal, "deck"
		if source.Trust == "guidance" {
			trust, kind = contextpack.TrustTrusted, contextpack.KindRepositoryInstructions
		}
		if source.Kind == "decision" && source.Trust == "guidance" {
			kind = contextpack.KindArchitectureDecision
		}
		if source.Kind == "git" {
			owner = "git"
		}
		if len(source.Content) > 16<<10 {
			return contextpack.Package{}, contextProblem("Source " + source.ID + " exceeds 16 KiB")
		}
		add(kind, uri, source.Title, owner, fmt.Sprint(source.Revision), source.Content, trust, expires)
	}
	for _, message := range resolved.Messages {
		uri := "message://" + message.ChannelID + "/" + message.ID
		if contextOverride(message.Content) {
			input.Omissions = append(input.Omissions, contextpack.Omission{URI: uri, Title: "Selected conversation message", Digest: message.Digest, Reason: "Message contains an instruction override"})
			continue
		}
		add(contextpack.KindExternal, uri, "Selected conversation message", "deck", message.Digest, message.Content, contextpack.TrustUntrusted, e.DeadlineAt)
	}
	manifest, err := contextpack.Build(now, input)
	if err != nil {
		return manifest, err
	}
	encoded, err := json.Marshal(manifest)
	if err == nil && len(encoded) > 128<<10 {
		return contextpack.Package{}, contextProblem("Assembled context exceeds 128 KiB")
	}
	return manifest, err
}

func (c *Controller) prepareContext(ctx context.Context, e execution.Execution, task workcontrol.WorkItem, s state) (string, error) {
	// Recover the package even if the process stopped before saving flow state.
	stored, err := c.Journal.ExecutionContextPackage(ctx, e.ID)
	if err == nil {
		if stored.SourceRevision != s.Base {
			return "", contextProblem("Saved context has a different repository revision")
		}
		return stored.Digest, nil
	}
	if !errors.Is(err, execution.ErrNotFound) {
		return "", err
	}
	resolved := workcontrol.ResolvedContext{}
	if task.Context != nil {
		provider, ok := c.Provider.(workcontrol.ContextInspection)
		if !ok {
			return "", contextProblem("Provider does not support selected project context")
		}
		resolved, err = provider.GetRunContext(ctx, e.ExternalRunID)
		if err != nil {
			return "", err
		}
		if err = workcontrol.ValidateResolvedContext(task.Context, resolved); err != nil {
			return "", contextProblem(err.Error())
		}
	}
	for i := range resolved.Sources {
		source := &resolved.Sources[i]
		if source.Kind != "git" {
			continue
		}
		port, err := c.Source(e)
		if err != nil {
			return "", err
		}
		reader, ok := port.(interface {
			ReadContextFile(context.Context, string, string, string) (string, error)
		})
		if !ok {
			return "", contextProblem("Source provider does not support pinned context files")
		}
		source.Content, err = reader.ReadContextFile(ctx, c.Config.Git.Repository, source.SourceRevision, source.Path)
		if err != nil {
			return "", err
		}
	}
	manifest, err := c.assembleContext(time.Now().UTC(), e, task, s, resolved)
	if err != nil {
		return "", err
	}
	if _, _, err = c.Journal.RecordContextPackage(ctx, manifest); err != nil {
		return "", err
	}
	return manifest.Digest, nil
}

func (c *Controller) publishContext(ctx context.Context, e execution.Execution, s state) error {
	manifest, err := c.Journal.GetContextPackage(ctx, s.ContextDigest)
	if err != nil {
		return err
	}
	// Publish the bounded derived snapshot for inspection. It contains selected
	// context and policy limits, never installation credential references.
	var outcome map[string]any
	for i := range manifest.Inputs {
		if manifest.Inputs[i].Kind == contextpack.KindTaskSnapshot {
			if err = json.Unmarshal([]byte(manifest.Inputs[i].Content), &outcome); err != nil {
				return err
			}
		}
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	var projection map[string]any
	if err = json.Unmarshal(encoded, &projection); err != nil {
		return err
	}
	_, _, err = c.Journal.EnqueueOutbox(ctx, e.ID, execution.OutboxInput{Kind: "event", IdempotencyKey: "context:" + e.ID, Payload: map[string]any{"type": "context.assembled", "message": "Authorized context assembled", "context": projection, "interpreted_outcome": outcome, "source_revision": s.Base, "occurred_at": manifest.CreatedAt.UnixMilli()}})
	return err
}
