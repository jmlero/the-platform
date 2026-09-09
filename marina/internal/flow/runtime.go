package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/execution"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

func (c *Controller) worker(ctx context.Context, e execution.Execution, task workcontrol.WorkItem, s state, stage string) (runtimeapi.Attempt, bool, worker.Result, string, error) {
	fail := func(err error) (runtimeapi.Attempt, bool, worker.Result, string, error) {
		return runtimeapi.Attempt{}, false, worker.Result{}, "", err
	}
	key := e.ID + ":" + stage
	items, err := c.Journal.ListRuntimeAttempts(ctx, e.ID)
	if err != nil {
		return fail(err)
	}
	var attempt runtimeapi.Attempt
	for _, item := range items {
		if item.IdempotencyKey == key {
			attempt = item
		}
	}
	if attempt.ID == "" {
		e, err = c.Journal.GetExecution(ctx, e.ID)
		if err != nil {
			return fail(err)
		}
		if len(items) >= e.MaxAttempts {
			return fail(execution.ErrBudgetExhausted)
		}
		_, attempt, _, err = c.Journal.EnsureRuntimeAttempt(ctx, e.ID, e.Version, runtimeapi.Specification{IdempotencyKey: key, RepositoryURI: c.Config.Git.CloneURL, SourceRevision: s.Head, ContextURI: "journal://" + e.ID, ContextDigest: c.contextDigest(task, s), HarnessVersion: c.Config.Kubernetes.Image, Limits: runtimeapi.Limits{CPUMillis: 2000, MemoryBytes: 4 << 30, EphemeralStorageBytes: 4 << 30, WallTimeSeconds: int64(c.Config.BudgetMinutes) * 60, MaxProcesses: 256}, NetworkPolicy: "gateway-only", FilesystemPolicy: "ephemeral-read-only-root", AuthorityFence: e.FenceToken, DeadlineAt: e.DeadlineAt, PolicyVersion: c.Config.PolicyVersion})
		if err != nil {
			return fail(err)
		}
	}
	if attempt.State == runtimeapi.StatePending {
		input := worker.Input{Stage: stage, SourceURL: strings.TrimRight(c.Config.PublicURL, "/") + "/workers/" + attempt.ID + "/git", SourceRevision: attempt.Specification.SourceRevision, Title: task.Title, Description: task.Description, AcceptanceCriteria: task.AcceptanceCriteria, AllowedPaths: c.Config.AllowedPaths, Checks: c.Config.Checks}
		if stage == "implement" {
			input.Harness = []string{"codex", "exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "-c", `web_search="disabled"`, "-c", `tools.view_image=false`, "-c", `model_provider="marina"`, "-c", `model="` + c.Config.Model.Name + `"`, "-c", `model_providers.marina.name="Marina controlled endpoint"`, "-c", `model_providers.marina.base_url="` + strings.TrimRight(c.Config.PublicURL, "/") + "/workers/" + attempt.ID + `/model/v1"`, "-c", `model_providers.marina.env_key="MARINA_WORKER_TOKEN"`, "-c", `model_providers.marina.wire_api="responses"`, "-"}
		}
		encoded, _ := json.Marshal(input)
		e, err = c.Journal.GetExecution(ctx, e.ID)
		if err != nil {
			return fail(err)
		}
		_, _, _, err = c.Journal.SaveCheckpoint(ctx, e.ID, e.Version, execution.CheckpointInput{Key: "worker-input:" + attempt.ID, Phase: "worker_input", SourceRevision: input.SourceRevision, Payload: map[string]any{"input": string(encoded)}})
		if err != nil {
			return fail(err)
		}
		attempt, err = c.Journal.MarkRuntimeStarting(ctx, attempt.ID, attempt.Version)
		if err != nil {
			return fail(err)
		}
	}
	if !attempt.State.Terminal() {
		var observation runtimeapi.Observation
		if attempt.ProviderHandle != "" {
			observation, err = c.Runtime.Inspect(ctx, attempt.ProviderHandle)
			if err == nil && !observation.Found {
				return fail(errors.New("previously bound worker disappeared; awaiting task deadline or cancellation"))
			}
		} else {
			observation, err = c.Runtime.InspectByKey(ctx, attempt.IdempotencyKey)
			if err == nil && !observation.Found {
				if err = c.Authorize(ctx, e, e.FenceToken); err != nil {
					return fail(err)
				}
				observation, err = c.Runtime.Start(ctx, attempt.Specification)
			}
		}
		if err != nil {
			return fail(err)
		}
		attempt, err = c.Journal.RecordRuntimeObservation(ctx, attempt.ID, attempt.Version, observation)
		if err != nil {
			return fail(err)
		}
	}
	if !attempt.State.Terminal() {
		return attempt, false, worker.Result{}, "", nil
	}
	if attempt.State != runtimeapi.StateSucceeded {
		return attempt, true, worker.Result{}, "", nil
	}
	artifacts, err := c.Journal.ListRuntimeArtifacts(ctx, e.ID)
	if err != nil {
		return fail(err)
	}
	for _, artifact := range artifacts {
		if artifact.RuntimeAttemptID == attempt.ID && artifact.Kind == "worker_result" {
			b, err := c.Artifacts.Get(artifact.Digest)
			if err != nil {
				return fail(err)
			}
			var result worker.Result
			if err = json.Unmarshal(b, &result); err != nil {
				return fail(err)
			}
			if result.SourceRevision != attempt.Specification.SourceRevision || result.Stage != stage || (stage == "verify" && len(result.Checks) != len(c.Config.Checks)) {
				return fail(errors.New("worker artifact does not match immutable input"))
			}
			return attempt, true, result, artifact.Digest, nil
		}
	}
	return fail(errors.New("successful worker has no durable result"))
}

func (c *Controller) stop(ctx context.Context, e execution.Execution) error {
	items, err := c.Journal.ListRuntimeAttempts(ctx, e.ID)
	if err != nil {
		return err
	}
	for _, attempt := range items {
		if attempt.State.Terminal() {
			continue
		}
		observation, err := c.Runtime.InspectByKey(ctx, attempt.IdempotencyKey)
		if err != nil {
			return err
		}
		if !observation.Found {
			observation = runtimeapi.Observation{Found: true, ProviderHandle: attempt.ProviderHandle, State: runtimeapi.StateCancelled}
			if observation.ProviderHandle == "" {
				observation.ProviderHandle = "never-started/" + attempt.ID
			}
		} else {
			observation, err = c.Runtime.Cancel(ctx, observation.ProviderHandle, true)
			if err != nil {
				return err
			}
		}
		if !observation.State.Terminal() {
			return errors.New("waiting for worker termination before acknowledging control")
		}
		if _, err = c.Journal.RecordRuntimeObservation(ctx, attempt.ID, attempt.Version, observation); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) Command(ctx context.Context, e execution.Execution, command workcontrol.Command) (string, string, error) {
	if command.Type == "cancel" {
		if err := c.stop(ctx, e); err != nil {
			return "", "", err
		}
		return "completed", "Worker termination confirmed", nil
	}
	// Resuming a killed harness as though it were suspended is unsafe. The first
	// release exposes cancellation and a new versioned task as the recovery path.
	return "rejected", "This release supports cancel; create a new task to retry or revise work", nil
}

func (c *Controller) cleanup(ctx context.Context) error {
	items, err := c.Journal.ListRuntimeCleanup(ctx)
	if err != nil {
		return err
	}
	for _, attempt := range items {
		e, err := c.Journal.GetExecution(ctx, attempt.ExecutionID)
		if err != nil {
			return err
		}
		if e.State.Terminal() && !attempt.State.Terminal() {
			if err = c.stop(ctx, e); err != nil {
				return err
			}
			return nil
		}
		if !attempt.State.Terminal() {
			continue
		}
		if attempt.ProviderHandle != "" && !strings.HasPrefix(attempt.ProviderHandle, "never-started/") {
			if err = c.Runtime.Cleanup(ctx, attempt.ProviderHandle); err != nil {
				return fmt.Errorf("cleanup %s: %w", attempt.ID, err)
			}
		}
		if _, err = c.Journal.MarkRuntimeCleanup(ctx, attempt.ID, attempt.Version, runtimeapi.CleanupCompleted, "Kubernetes Job and attempt credential removed; artifacts retained"); err != nil {
			return err
		}
	}
	return nil
}
