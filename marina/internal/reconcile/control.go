package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

type ControlProvider interface {
	workcontrol.HealthChecker
	workcontrol.WorkDiscovery
	workcontrol.WorkInspection
	workcontrol.RecoverableAcquisition
	workcontrol.CommandControl
	workcontrol.ProjectionPublisher
}

type ControlJournal interface {
	EnsureAcquisitionIntent(context.Context, execution.NewAcquisitionIntent) (execution.AcquisitionIntent, bool, error)
	ListUnresolvedAcquisitions(context.Context, int) ([]execution.AcquisitionIntent, error)
	RecordAcquisitionObservation(context.Context, string, int64, int64, string, *time.Time) error
	RecordAcquisitionClaim(context.Context, string, int64, int64, string, time.Time) (execution.AcquisitionIntent, error)
	MarkAcquisitionBound(context.Context, string, string) error
	RecordAcquisitionFailure(context.Context, string, execution.FailureClass, string, int) error
	EnsureExecution(context.Context, execution.NewExecution) (execution.Execution, bool, error)
	GetExecution(context.Context, string) (execution.Execution, error)
	ListActiveExecutions(context.Context, int) ([]execution.Execution, error)
	ListCommandExecutions(context.Context, int) ([]execution.Execution, error)
	CountActiveExecutionsForProject(context.Context, string) (int, error)
	UpdateProviderAuthority(context.Context, string, int64, int64, int64, time.Time, string) (execution.Execution, error)
	Transition(context.Context, string, execution.Transition) (execution.Execution, error)
	SetCommandCursor(context.Context, string, int64, int64) (execution.Execution, error)
	EnqueueOutbox(context.Context, string, execution.OutboxInput) (execution.OutboxItem, bool, error)
	ListReadyOutbox(context.Context, int) ([]execution.OutboxItem, error)
	SaveOutboxGuard(context.Context, string, *execution.OutboxGuard, execution.OutboxGuard) (execution.OutboxItem, error)
	MarkOutboxPublished(context.Context, string) error
	CompleteOutboxPublication(context.Context, string, string, int64, int64, int64, time.Time, string) (execution.Execution, error)
	RecordOutboxFailure(context.Context, string, execution.FailureClass, string, int) error
}

type ControlCycle struct {
	Acquisitions int
	Commands     int
	Published    int
	Errors       []error
}

type ControlController struct {
	provider          ControlProvider
	journal           ControlJournal
	binding           execution.ProjectBinding
	identity          execution.InstallationIdentity
	lease             time.Duration
	operationTimeout  time.Duration
	safetyWindow      time.Duration
	maxRetries        int
	clock             func() time.Time
	cycleMu           sync.Mutex
	executionTokens   int64
	executionAttempts int
	intakeDisabled    bool
	intakeWorkID      string
	commandHandler    func(context.Context, execution.Execution, workcontrol.Command) (string, string, error)
}

// WithIntakeEnabled controls only new discovery. Already journaled acquisition
// intents, commands and progress still reconcile when intake is disabled.
// Configure this before starting controller cycles.
func (controller *ControlController) WithIntakeEnabled(enabled bool) *ControlController {
	controller.intakeDisabled = !enabled
	return controller
}

// WithIntakeWorkID limits new discovery to one work item. Empty allows all ready
// work. Recovery and command reconciliation remain independent of this filter.
// Configure this before starting controller cycles.
func (controller *ControlController) WithIntakeWorkID(id string) *ControlController {
	controller.intakeWorkID = id
	return controller
}

func (controller *ControlController) WithExecutionLimits(attempts int, tokens int64) *ControlController {
	controller.executionAttempts = attempts
	controller.executionTokens = tokens
	return controller
}

func (controller *ControlController) WithCommandHandler(handler func(context.Context, execution.Execution, workcontrol.Command) (string, string, error)) *ControlController {
	controller.commandHandler = handler
	return controller
}

func NewControlController(provider ControlProvider, journal ControlJournal, binding execution.ProjectBinding,
	identity execution.InstallationIdentity, lease time.Duration) (*ControlController, error) {
	if provider == nil || journal == nil || binding.ProjectID == "" || binding.Version < 1 ||
		identity.InstallationID == "" || identity.LogicalAgentID == "" || lease < time.Second {
		return nil, errors.New("provider, journal, versioned binding, stable identity, and lease are required")
	}
	safetyWindow := 5 * time.Second
	if lease/3 < safetyWindow {
		safetyWindow = lease / 3
	}
	return &ControlController{provider: provider, journal: journal, binding: binding, identity: identity,
		lease: lease, operationTimeout: 5 * time.Second, safetyWindow: safetyWindow, maxRetries: 5, clock: time.Now}, nil
}

func (controller *ControlController) Cycle(ctx context.Context) ControlCycle {
	cycle := ControlCycle{}
	if !controller.cycleMu.TryLock() {
		cycle.Errors = append(cycle.Errors, fmt.Errorf("control cycle is already running: %w", execution.ErrConflict))
		return cycle
	}
	defer controller.cycleMu.Unlock()
	controller.recoverAcquisitions(ctx, &cycle)
	if !controller.intakeDisabled {
		controller.intake(ctx, &cycle)
	}
	controller.consumeCommands(ctx, &cycle)
	controller.publishOutbox(ctx, &cycle)
	return cycle
}

func (controller *ControlController) recoverAcquisitions(ctx context.Context, cycle *ControlCycle) {
	intents, err := controller.journal.ListUnresolvedAcquisitions(ctx, 100)
	if err != nil {
		cycle.Errors = append(cycle.Errors, err)
		return
	}
	for _, intent := range intents {
		if err := controller.processAcquisition(ctx, intent); err != nil {
			cycle.Errors = append(cycle.Errors, err)
		} else {
			cycle.Acquisitions++
		}
	}
}

func (controller *ControlController) intake(ctx context.Context, cycle *ControlCycle) {
	active, err := controller.journal.CountActiveExecutionsForProject(ctx, controller.binding.ProjectID)
	if err != nil {
		cycle.Errors = append(cycle.Errors, err)
		return
	}
	if active >= controller.binding.MaxConcurrency {
		return
	}
	operationCtx, cancel := context.WithTimeout(ctx, controller.operationTimeout)
	defer cancel()
	items, err := controller.provider.ListReadyWork(operationCtx)
	if err != nil {
		cycle.Errors = append(cycle.Errors, err)
		return
	}
	for _, item := range items {
		if controller.intakeWorkID != "" && item.ID != controller.intakeWorkID {
			continue
		}
		if item.ProjectID != controller.binding.ProjectID || active >= controller.binding.MaxConcurrency {
			continue
		}
		digest := workDigest(item)
		snapshot, snapshotErr := json.Marshal(item)
		if snapshotErr != nil {
			cycle.Errors = append(cycle.Errors, snapshotErr)
			continue
		}
		acceptanceDigest := sha256.Sum256([]byte(item.AcceptanceCriteria))
		intent, created, err := controller.journal.EnsureAcquisitionIntent(ctx, execution.NewAcquisitionIntent{
			TaskSnapshot: string(snapshot),
			ProjectID:    item.ProjectID, WorkID: item.ID, TaskVersion: item.Version, TaskDigest: digest,
			AcceptanceCriteriaDigest: hex.EncodeToString(acceptanceDigest[:]),
			ClaimKey:                 fmt.Sprintf("claim:%s:%s:v%d", item.ProjectID, item.ID, item.Version),
			AgentID:                  controller.identity.LogicalAgentID, LeaseSeconds: int(controller.lease / time.Second),
			BudgetMinutes: min(item.BudgetMinutes, controller.binding.BudgetMinutes),
		})
		if err != nil {
			cycle.Errors = append(cycle.Errors, err)
			continue
		}
		if !created {
			continue
		}
		if err := controller.processAcquisition(ctx, intent); err != nil {
			cycle.Errors = append(cycle.Errors, err)
			continue
		}
		cycle.Acquisitions++
		active++
	}
}

func workDigest(item workcontrol.WorkItem) string {
	encoded, _ := json.Marshal(struct {
		ID                             string
		Version                        int
		Title, Description, Acceptance string
		Budget                         int
	}{
		item.ID, item.Version, item.Title, item.Description, item.AcceptanceCriteria, item.BudgetMinutes})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (controller *ControlController) processAcquisition(ctx context.Context, intent execution.AcquisitionIntent) error {
	if intent.Status == "pending" {
		operationCtx, cancel := context.WithTimeout(ctx, controller.operationTimeout)
		run, err := controller.provider.LookupClaim(operationCtx, intent.ProjectID, intent.ClaimKey)
		cancel()
		if err != nil {
			pageCtx, pageCancel := context.WithTimeout(ctx, controller.operationTimeout)
			page, pageErr := controller.provider.ListAssignedRuns(pageCtx, intent.ProjectID, intent.AgentID, "", 100)
			pageCancel()
			if pageErr == nil {
				for _, assigned := range page.Items {
					if assigned.Work.ID == intent.WorkID && assigned.Run.TaskVersion == intent.TaskVersion {
						run = assigned.Run
						err = nil
						break
					}
				}
			}
		}
		if err != nil {
			operationCtx, cancel = context.WithTimeout(ctx, controller.operationTimeout)
			run, err = controller.provider.ClaimWork(operationCtx, intent.WorkID, workcontrol.Claim{
				IdempotencyKey: intent.ClaimKey, AgentID: intent.AgentID, ExpectedTaskVersion: intent.TaskVersion,
				Lease: time.Duration(intent.LeaseSeconds) * time.Second})
			cancel()
		}
		if err != nil {
			class := classifyExternal(err)
			_ = controller.journal.RecordAcquisitionFailure(ctx, intent.ID, class, err.Error(), controller.maxRetries)
			return fmt.Errorf("acquire work %s: %w", intent.WorkID, err)
		}
		if err := controller.recordClaimObservation(ctx, intent.ID, run); err != nil {
			return err
		}
		if err = controller.validateClaimResult(intent, run); err != nil {
			_ = controller.journal.RecordAcquisitionFailure(ctx, intent.ID, execution.FailurePermanent, err.Error(), controller.maxRetries)
			return err
		}
		var recordErr error
		intent, recordErr = controller.journal.RecordAcquisitionClaim(ctx, intent.ID, run.Version, run.FenceToken, run.ID, *run.LeaseExpiresAt)
		if recordErr != nil {
			return recordErr
		}
	}
	if intent.Status != "claimed" || intent.LeaseExpiresAt == nil {
		return nil
	}
	// A crash can occur after the provider result is journaled but before the
	// execution is bound. Re-read the claim's current authority epoch here so a
	// lease reaper or operator fence in that window cannot be resurrected.
	operationCtx, cancel := context.WithTimeout(ctx, controller.operationTimeout)
	current, err := controller.provider.LookupClaim(operationCtx, intent.ProjectID, intent.ClaimKey)
	cancel()
	if err != nil {
		class := classifyExternal(err)
		_ = controller.journal.RecordAcquisitionFailure(ctx, intent.ID, class, err.Error(), controller.maxRetries)
		return fmt.Errorf("confirm claimed work %s before binding: %w", intent.WorkID, err)
	}
	if err := controller.recordClaimObservation(ctx, intent.ID, current); err != nil {
		return err
	}
	if err := controller.validateClaimResult(intent, current); err != nil {
		_ = controller.journal.RecordAcquisitionFailure(ctx, intent.ID, execution.FailurePermanent, err.Error(), controller.maxRetries)
		return err
	}
	intent, err = controller.journal.RecordAcquisitionClaim(ctx, intent.ID, current.Version, current.FenceToken, current.ID, *current.LeaseExpiresAt)
	if err != nil {
		return err
	}
	deadline := intent.CreatedAt.Add(time.Duration(intent.BudgetMinutes) * time.Minute)
	budget := execution.Budget{DeadlineAt: deadline, MaxAttempts: controller.maxRetries, MaxTokens: 1}
	if controller.executionTokens > 0 && controller.executionAttempts > 0 {
		var task workcontrol.WorkItem
		if err := json.Unmarshal([]byte(intent.TaskSnapshot), &task); err != nil {
			return fmt.Errorf("runtime intake requires a frozen task snapshot: %w", err)
		}
		budget.MaxAttempts = controller.executionAttempts
		budget.MaxTokens = controller.executionTokens
		if task.Budget.MaxAttempts > 0 {
			budget.MaxAttempts = min(budget.MaxAttempts, task.Budget.MaxAttempts)
		}
		if task.Budget.MaxTokens > 0 {
			budget.MaxTokens = min(budget.MaxTokens, task.Budget.MaxTokens)
		}
		if task.Budget.MaxCostMinor > 0 && task.Budget.MaxCostMinor <= 1<<40 {
			budget.MaxCostMicros = task.Budget.MaxCostMinor * 10000
		}
		if task.Budget.DeadlineAt != nil && task.Budget.DeadlineAt.Before(budget.DeadlineAt) {
			budget.DeadlineAt = *task.Budget.DeadlineAt
		}
	}
	bindingVersion := controller.binding.Version
	var boundTask workcontrol.WorkItem
	if intent.TaskSnapshot != "" {
		if err := json.Unmarshal([]byte(intent.TaskSnapshot), &boundTask); err != nil {
			return err
		}
	}
	if boundTask.Context != nil && boundTask.Context.Repository != nil {
		bindingVersion = boundTask.Context.Repository.Version
		if bindingVersion < 1 {
			return execution.ErrInvalidInput
		}
	}
	record, _, err := controller.journal.EnsureExecution(ctx, execution.NewExecution{
		WorkProvider: controller.binding.Provider, ProjectID: intent.ProjectID, ExternalWorkID: intent.WorkID,
		ExternalRunID: intent.ExternalRunID, AgentID: intent.AgentID, ExternalRunVersion: intent.ExternalVersion,
		FenceToken: intent.FenceToken, TaskVersion: intent.TaskVersion, TaskDigest: intent.TaskDigest,
		BindingVersion: bindingVersion, LeaseExpiresAt: *intent.LeaseExpiresAt,
		Budget: budget,
	})
	if err != nil {
		return err
	}
	if _, _, err := controller.journal.EnqueueOutbox(ctx, record.ID, execution.OutboxInput{Kind: "event",
		IdempotencyKey: "projection:claimed:" + record.ID, Payload: map[string]any{"type": "execution.claimed",
			"message": "Marina durably bound the claimed run", "occurred_at": controller.clock().UTC().UnixMilli()}}); err != nil {
		return err
	}
	return controller.journal.MarkAcquisitionBound(ctx, intent.ID, intent.ExternalRunID)
}

func (controller *ControlController) recordClaimObservation(ctx context.Context, intentID string, run workcontrol.Run) error {
	if run.ID == "" || run.Version < 1 || run.FenceToken < 1 {
		return nil
	}
	return controller.journal.RecordAcquisitionObservation(ctx, intentID, run.Version, run.FenceToken, run.ID, run.LeaseExpiresAt)
}

func (controller *ControlController) validateClaimResult(intent execution.AcquisitionIntent, run workcontrol.Run) error {
	if run.WorkItemID != intent.WorkID || run.AgentID != intent.AgentID || run.TaskVersion != intent.TaskVersion ||
		run.AcceptanceCriteriaDigest != intent.AcceptanceCriteriaDigest || run.Version < 1 || run.FenceToken < 1 ||
		!run.Status.Active() || run.LeaseExpiresAt == nil ||
		!run.LeaseExpiresAt.After(controller.clock().UTC().Add(controller.safetyWindow)) {
		return fmt.Errorf("claim result is stale or does not match durable intent: %w", execution.ErrConflict)
	}
	return nil
}

func (controller *ControlController) consumeCommands(ctx context.Context, cycle *ControlCycle) {
	records, err := controller.journal.ListCommandExecutions(ctx, 1000)
	if err != nil {
		cycle.Errors = append(cycle.Errors, err)
		return
	}
	for _, record := range records {
		operationCtx, cancel := context.WithTimeout(ctx, controller.operationTimeout)
		commands, err := controller.provider.ListRunCommands(operationCtx, record.ExternalRunID, record.CommandCursor, 100)
		cancel()
		if err != nil {
			cycle.Errors = append(cycle.Errors, err)
			continue
		}
		for _, command := range commands {
			if command.Sequence <= record.CommandCursor {
				continue
			}
			if record.LeaseExpiresAt.IsZero() {
				cycle.Errors = append(cycle.Errors, execution.ErrConflict)
				break
			}
			// Command creation records the epoch at request time. A later
			// heartbeat can advance the live run past that pair; using the
			// stored pair as current authority would rewind and never ack.
			current, liveErr := controller.currentProviderRun(ctx, record)
			if liveErr != nil {
				cycle.Errors = append(cycle.Errors, liveErr)
				break
			}
			if current.Version < command.RunVersion || current.FenceToken < command.FenceToken {
				cycle.Errors = append(cycle.Errors, fmt.Errorf("provider run is behind command epoch: %w", execution.ErrConflict))
				break
			}
			lease := record.LeaseExpiresAt
			if current.LeaseExpiresAt != nil {
				lease = *current.LeaseExpiresAt
			}
			if record.ExternalRunVersion != current.Version || record.FenceToken != current.FenceToken {
				record, err = controller.journal.UpdateProviderAuthority(ctx, record.ID, record.Version,
					current.Version, current.FenceToken, lease, "live provider authority observed before command acknowledgement")
				if err != nil {
					cycle.Errors = append(cycle.Errors, err)
					break
				}
			}
			status, result := commandOutcome(command.Type)
			if controller.commandHandler != nil {
				status, result, err = controller.commandHandler(ctx, record, command)
				if err != nil {
					cycle.Errors = append(cycle.Errors, err)
					break
				}
				record, err = controller.journal.GetExecution(ctx, record.ID)
				if err != nil {
					cycle.Errors = append(cycle.Errors, err)
					break
				}
			}
			if command.Type == "cancel" && !record.State.Terminal() {
				record, err = controller.journal.Transition(ctx, record.ID, execution.Transition{ExpectedVersion: record.Version,
					Target: execution.StateCancelled, Message: "operator cancellation pre-empted ordinary progress"})
				if err != nil {
					cycle.Errors = append(cycle.Errors, err)
					break
				}
			}
			_, _, err = controller.journal.EnqueueOutbox(ctx, record.ID, execution.OutboxInput{Kind: "command_ack",
				IdempotencyKey: "command-ack:" + command.ID, Payload: map[string]any{"command_id": command.ID,
					"status": status, "result": result}})
			if err != nil {
				cycle.Errors = append(cycle.Errors, err)
				break
			}
			record, err = controller.journal.SetCommandCursor(ctx, record.ID, record.Version, command.Sequence)
			if err != nil {
				cycle.Errors = append(cycle.Errors, err)
				break
			}
			cycle.Commands++
			if command.Type == "cancel" {
				break
			}
		}
	}
}

func commandOutcome(commandType string) (string, string) {
	switch commandType {
	case "cancel":
		return "completed", "authority revoked and local execution cancelled"
	case "pause":
		return "completed", "execution checkpointed; no runtime is enabled"
	case "reduce_authority":
		return "completed", "reduced authority fence accepted"
	case "replan":
		return "completed", "replan recorded before runtime enablement"
	case "resume", "retry":
		return "rejected", "no paused or terminal runtime attempt is eligible"
	default:
		return "rejected", "unsupported command"
	}
}

func (controller *ControlController) publishOutbox(ctx context.Context, cycle *ControlCycle) {
	items, err := controller.journal.ListReadyOutbox(ctx, 100)
	if err != nil {
		cycle.Errors = append(cycle.Errors, err)
		return
	}
	for _, item := range items {
		record, err := controller.journal.GetExecution(ctx, item.ExecutionID)
		if err != nil {
			cycle.Errors = append(cycle.Errors, err)
			continue
		}
		if item.Guard == nil {
			item, err = controller.journal.SaveOutboxGuard(ctx, item.ID, nil, execution.OutboxGuard{
				AgentID: record.AgentID, ExpectedVersion: record.ExternalRunVersion, FenceToken: record.FenceToken})
			if err != nil {
				cycle.Errors = append(cycle.Errors, err)
				continue
			}
		}
		operationCtx, cancel := context.WithTimeout(ctx, controller.operationTimeout)
		switch item.Kind {
		case "command_ack":
			err = controller.provider.AcknowledgeRunCommand(operationCtx, record.ExternalRunID,
				stringValue(item.Payload, "command_id"), workcontrol.CommandAcknowledgement{IdempotencyKey: item.IdempotencyKey,
					AgentID: item.Guard.AgentID, ExpectedVersion: item.Guard.ExpectedVersion, FenceToken: item.Guard.FenceToken,
					Status: stringValue(item.Payload, "status"), Result: stringValue(item.Payload, "result")})
		default:
			err = controller.provider.AppendRunEvent(operationCtx, record.ExternalRunID, workcontrol.ProjectedEvent{
				IdempotencyKey: item.IdempotencyKey, AgentID: item.Guard.AgentID, ExpectedVersion: item.Guard.ExpectedVersion,
				FenceToken: item.Guard.FenceToken, Producer: "marina", ProducerEventID: item.ID,
				ExecutionID: record.ID, Sequence: item.Sequence, Type: stringValue(item.Payload, "type"),
				Message: stringValue(item.Payload, "message"), Payload: item.Payload,
				OccurredAt: time.UnixMilli(int64Value(item.Payload, "occurred_at", item.CreatedAt.UnixMilli())).UTC()})
		}
		cancel()
		if err != nil {
			class := classifyExternal(err)
			var conflict interface{ MutationConflict() bool }
			if errors.As(err, &conflict) && conflict.MutationConflict() {
				refreshed, refreshErr := controller.refreshRejectedOutbox(ctx, record, item)
				if refreshErr != nil {
					err, class = refreshErr, classifyExternal(refreshErr)
				} else if refreshed {
					class = execution.FailureTransient
				}
			}
			_ = controller.journal.RecordOutboxFailure(ctx, item.ID, class, err.Error(), controller.maxRetries)
			cycle.Errors = append(cycle.Errors, err)
			continue
		}
		if record.State.Terminal() && item.Kind != "command_ack" {
			if err := controller.journal.MarkOutboxPublished(ctx, item.ID); err != nil {
				cycle.Errors = append(cycle.Errors, err)
				continue
			}
			cycle.Published++
			continue
		}
		current, inspectErr := controller.currentProviderRun(ctx, record)
		if inspectErr != nil {
			_ = controller.journal.RecordOutboxFailure(ctx, item.ID, classifyExternal(inspectErr), inspectErr.Error(), controller.maxRetries)
			cycle.Errors = append(cycle.Errors, inspectErr)
			continue
		}
		lease := record.LeaseExpiresAt
		if current.LeaseExpiresAt != nil {
			lease = *current.LeaseExpiresAt
		}
		if _, err := controller.journal.CompleteOutboxPublication(ctx, item.ID, record.ID, record.Version,
			current.Version, current.FenceToken, lease, "provider projection acknowledged"); err != nil {
			cycle.Errors = append(cycle.Errors, err)
			continue
		}
		cycle.Published++
	}
}

func (controller *ControlController) refreshRejectedOutbox(ctx context.Context, record execution.Execution, item execution.OutboxItem) (bool, error) {
	// Exact replay would have succeeded if the original request had committed.
	// After a definitive rejection, a heartbeat may permit a new version. Never
	// transfer a queued mutation across an ownership fence or to another agent.
	current, err := controller.currentProviderRun(ctx, record)
	if err != nil {
		return false, err
	}
	if current.WorkItemID != record.ExternalWorkID || current.AgentID != item.Guard.AgentID ||
		current.FenceToken != item.Guard.FenceToken || current.Version <= item.Guard.ExpectedVersion {
		return false, nil
	}
	_, err = controller.journal.SaveOutboxGuard(ctx, item.ID, item.Guard, execution.OutboxGuard{
		AgentID: current.AgentID, ExpectedVersion: current.Version, FenceToken: current.FenceToken})
	return err == nil, err
}

func (controller *ControlController) currentProviderRun(ctx context.Context, record execution.Execution) (workcontrol.Run, error) {
	inspectCtx, inspectCancel := context.WithTimeout(ctx, controller.operationTimeout)
	defer inspectCancel()
	detail, err := controller.provider.GetWork(inspectCtx, record.ExternalWorkID)
	if err != nil {
		return workcontrol.Run{}, err
	}
	for _, candidate := range detail.Runs {
		if candidate.ID == record.ExternalRunID {
			return candidate, nil
		}
	}
	return workcontrol.Run{}, execution.ErrConflict
}

func classifyExternal(err error) execution.FailureClass {
	if err == nil {
		return ""
	}
	var retryable interface{ RetryableFailure() bool }
	if errors.As(err, &retryable) && retryable.RetryableFailure() {
		return execution.FailureTransient
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return execution.FailureTransient
	}
	var network net.Error
	if errors.As(err, &network) {
		return execution.FailureTransient
	}
	return execution.FailurePermanent
}

func stringValue(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}
func int64Value(payload map[string]any, key string, fallback int64) int64 {
	switch value := payload[key].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	}
	return fallback
}
