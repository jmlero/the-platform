// Package kagent implements the pinned engine lifecycle at Marina's runtime
// boundary. It is deliberately not wired into governed workloads until KINT-04–07.
package kagent

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2a "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	api "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

const Version = "1.0.0-alpha1@850c464726881317997c32515f8b621b949d6ce1"

var (
	ErrDeliveryUnknown     = errors.New("kagent message delivery is uncertain; inspect the recorded instance, do not resend")
	ErrRevisionMismatch    = errors.New("kagent prepared revision differs from authorized revision")
	ErrIdentity            = errors.New("kagent identity missing or changed; replacement work is forbidden")
	ErrInteractionRequired = errors.New("kagent task needs unsupported interaction; KINT-08 required")
)

type Journal interface {
	GetRuntimeAttempt(context.Context, string) (rt.Attempt, error)
	EngineAttemptByKey(context.Context, string) (rt.Attempt, error)
	EnsureEngineAttempt(context.Context, rt.Specification) (rt.EngineAttempt, error)
	GetEngineAttempt(context.Context, string) (rt.EngineAttempt, error)
	UpdateEngineAttempt(context.Context, rt.EngineAttempt) (rt.EngineAttempt, error)
	RecordRuntimeArtifact(context.Context, rt.Artifact) (rt.Artifact, bool, error)
	ListRuntimeArtifacts(context.Context, string) ([]rt.Artifact, error)
}

// Admission is Marina's current authority check, including Deck and selected
// context revocation. It is required before creating or sending work, also on
// recovery. Observation and cancellation must not depend on fresh authority.
type Admission interface {
	AdmitEngine(context.Context, rt.Specification) error
}
type Adapter struct {
	instances api.AgentInstanceServiceClient
	tasks     a2a.A2AServiceClient
	endpoint  string
	journal   Journal
	blobs     *artifacts.Store
	admission Admission
}

var _ rt.Adapter = (*Adapter)(nil)

// The caller owns the connection and its authenticated transport. Endpoint is
// the installed endpoint identity bound in each immutable specification.
func New(conn grpc.ClientConnInterface, endpoint string, journal Journal, blobs *artifacts.Store, admission Admission) *Adapter {
	return &Adapter{instances: api.NewAgentInstanceServiceClient(conn), tasks: a2a.NewA2AServiceClient(conn), endpoint: endpoint, journal: journal, blobs: blobs, admission: admission}
}
func (a *Adapter) admit(ctx context.Context, s rt.Specification) error {
	if a.admission == nil {
		return errors.New("kagent dispatch requires governed admission")
	}
	return a.admission.AdmitEngine(ctx, s)
}
func (*Adapter) Capabilities() rt.Capabilities { return rt.Capabilities{} }
func (*Adapter) Resume(context.Context, string) (rt.Observation, error) {
	return rt.Observation{}, rt.ErrUnsupportedResume
}
func observation(r rt.EngineAttempt) rt.Observation {
	return rt.Observation{Found: true, ProviderHandle: "kagent:" + r.AttemptID, State: r.State}
}
func (a *Adapter) save(ctx context.Context, r rt.EngineAttempt) (rt.EngineAttempt, error) {
	return a.journal.UpdateEngineAttempt(ctx, r)
}
func (a *Adapter) selection(s rt.Specification) error {
	if s.Engine == nil || s.Engine.Backend != "kagent" || s.Engine.Version != Version || s.Engine.Endpoint != a.endpoint {
		return rt.ErrConflict
	}
	return s.Engine.Validate()
}
func (a *Adapter) Start(ctx context.Context, s rt.Specification) (rt.Observation, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := a.selection(s); err != nil {
		return rt.Observation{}, err
	}
	// Validate before any side effect, including recovery from a lost start.
	if err := s.Validate(time.Now()); err != nil {
		return rt.Observation{}, err
	}
	r, err := a.journal.EnsureEngineAttempt(ctx, s)
	if err != nil {
		return rt.Observation{}, err
	}
	return a.advance(ctx, s, r)
}
func (a *Adapter) InspectByKey(ctx context.Context, key string) (rt.Observation, error) {
	attempt, err := a.journal.EngineAttemptByKey(ctx, key)
	if errors.Is(err, execution.ErrNotFound) {
		return rt.Observation{}, nil
	}
	if err != nil {
		return rt.Observation{}, err
	}
	return a.Inspect(ctx, "kagent:"+attempt.ID)
}
func (a *Adapter) load(ctx context.Context, handle string) (rt.Specification, rt.EngineAttempt, error) {
	id, ok := strings.CutPrefix(handle, "kagent:")
	if !ok || id == "" {
		return rt.Specification{}, rt.EngineAttempt{}, rt.ErrInvalidInput
	}
	attempt, err := a.journal.GetRuntimeAttempt(ctx, id)
	if err != nil {
		return rt.Specification{}, rt.EngineAttempt{}, err
	}
	s := attempt.Specification
	if err = a.selection(s); err != nil {
		return s, rt.EngineAttempt{}, err
	}
	r, err := a.journal.GetEngineAttempt(ctx, id)
	if errors.Is(err, execution.ErrNotFound) {
		r, err = a.journal.EnsureEngineAttempt(ctx, s)
	}
	return s, r, err
}
func (a *Adapter) Inspect(ctx context.Context, handle string) (rt.Observation, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	s, r, err := a.load(ctx, handle)
	if err != nil {
		return rt.Observation{}, err
	}
	return a.advance(ctx, s, r)
}
func (a *Adapter) advance(ctx context.Context, s rt.Specification, r rt.EngineAttempt) (rt.Observation, error) {
	if r.Deleted {
		return observation(r), nil
	}
	if r.CancelRequested {
		return a.cancel(ctx, s, r)
	}
	if r.DeleteRequested {
		return observation(r), a.delete(ctx, r)
	}
	if r.Phase == "revision_mismatch" {
		return observation(r), ErrRevisionMismatch
	}
	if r.InstanceID == "" {
		if err := a.admit(ctx, s); err != nil {
			return observation(r), err
		}
		if !s.DeadlineAt.After(time.Now()) {
			return observation(r), fmt.Errorf("engine start authority expired: %w", rt.ErrConflict)
		}
		// Replay only creation with its original key. This API has no lookup-by-key RPC.
		out, err := a.instances.CreateAgentInstance(ctx, &api.CreateAgentInstanceRequest{Harness: &api.ResourceReference{Namespace: s.Engine.Namespace, Name: s.Engine.Harness}, AgentTemplate: &api.ResourceReference{Namespace: s.Engine.Namespace, Name: s.Engine.Template}, RequestId: s.IdempotencyKey})
		if err != nil {
			return observation(r), rpcError("create instance", err)
		}
		instance := out.GetAgentInstance()
		if instance.GetId() == "" || instance.GetContextId() == "" || instance.GetA2AAuthority() == "" {
			return observation(r), ErrIdentity
		}
		r.InstanceID = instance.Id
		r.ContextID = instance.ContextId
		r.Route = instance.A2AAuthority
		r.Revision = instance.PreparedRevision
		r.Phase = "ready"
		if instance.PreparedRevision != s.Engine.Revision || !matchesSelection(instance, s) {
			r.Phase = "revision_mismatch"
		}
		r, err = a.save(ctx, r)
		if err != nil {
			return observation(r), err
		}
		if r.Phase == "revision_mismatch" {
			return observation(r), ErrRevisionMismatch
		}
	}
	instance, err := a.instances.GetAgentInstance(ctx, &api.GetAgentInstanceRequest{AgentInstanceId: r.InstanceID})
	if status.Code(err) == codes.NotFound {
		return observation(r), ErrIdentity
	}
	if err != nil {
		return observation(r), rpcError("inspect instance", err)
	}
	i := instance.GetAgentInstance()
	if !matchesRecord(i, r) || !matchesSelection(i, s) {
		return observation(r), ErrIdentity
	}
	if r.Phase == "ready" {
		if i.State != api.AgentInstanceState_AGENT_INSTANCE_STATE_READY || i.Operation != api.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
			return observation(r), fmt.Errorf("engine instance not ready: %w", rt.ErrConflict)
		}
		if err = s.Validate(time.Now()); err != nil {
			return observation(r), err
		}
		messageDigest, ok := strings.CutPrefix(s.ContextURI, "artifact://")
		if !ok {
			return observation(r), fmt.Errorf("engine message must be an immutable artifact: %w", rt.ErrInvalidInput)
		}
		body, err := a.blobs.Get(messageDigest)
		if err != nil {
			return observation(r), err
		}
		if err := a.admit(ctx, s); err != nil {
			return observation(r), err
		}
		// Persist send intent before crossing the network. Even a crash before send
		// becomes an inspect-only uncertainty; availability never justifies duplication.
		r.Phase = "sending"
		r, err = a.save(ctx, r)
		if err != nil {
			return observation(r), err
		}
		response, err := a.tasks.SendMessage(route(ctx, r), &a2a.SendMessageRequest{Message: &a2a.Message{MessageId: r.MessageID, ContextId: r.ContextID, Role: a2a.Role_ROLE_USER, Parts: []*a2a.Part{{Content: &a2a.Part_Text{Text: string(body)}}}}}, grpc.MaxCallRecvMsgSize(artifacts.MaxBytes))
		if err != nil {
			return observation(r), rpcError("send message (delivery uncertain)", err)
		}
		if task := response.GetTask(); task != nil {
			return a.bindTask(ctx, r, task)
		}
		// Message-only replies are not success evidence. Recover the stored task.
	}
	if r.TaskID == "" {
		return a.discoverTask(ctx, r)
	}
	task, err := a.tasks.GetTask(route(ctx, r), &a2a.GetTaskRequest{Id: r.TaskID}, grpc.MaxCallRecvMsgSize(artifacts.MaxBytes))
	if status.Code(err) == codes.NotFound {
		return observation(r), ErrIdentity
	}
	if err != nil {
		return observation(r), rpcError("inspect task", err)
	}
	return a.bindTask(ctx, r, task)
}
func matchesSelection(i *api.AgentInstance, s rt.Specification) bool {
	return i.GetHarness().GetName() == s.Engine.Harness && i.GetHarness().GetNamespace() == s.Engine.Namespace && i.GetAgentTemplate().GetName() == s.Engine.Template && i.GetAgentTemplate().GetNamespace() == s.Engine.Namespace
}
func matchesRecord(i *api.AgentInstance, r rt.EngineAttempt) bool {
	return i.GetId() == r.InstanceID && i.GetContextId() == r.ContextID && i.GetPreparedRevision() == r.Revision && i.GetA2AAuthority() == r.Route
}
func route(ctx context.Context, r rt.EngineAttempt) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "x-kagent-agent-instance-id", r.InstanceID, "a2a-version", "1.0",
		strings.ToLower(a2atype.SvcParamExtensions), kagenta2a.HITLExtensionURI)
}
func rpcError(action string, err error) error {
	return fmt.Errorf("kagent %s: %s", action, status.Code(err))
}
func (a *Adapter) discoverTask(ctx context.Context, r rt.EngineAttempt) (rt.Observation, error) {
	// This phase owns exactly one initial message and no continuations. Bounded
	// pages with full history recover that message without replaying SendMessage.
	var found *a2a.Task
	token := ""
	for page := 0; page < 10; page++ {
		size := int32(100)
		out, err := a.tasks.ListTasks(route(ctx, r), &a2a.ListTasksRequest{ContextId: r.ContextID, PageSize: &size, PageToken: token}, grpc.MaxCallRecvMsgSize(artifacts.MaxBytes))
		if err != nil {
			return observation(r), rpcError("lookup message", err)
		}
		for _, task := range out.Tasks {
			for _, message := range task.History {
				if message.GetMessageId() == r.MessageID {
					if found != nil && found.Id != task.Id {
						return observation(r), ErrIdentity
					}
					found = task
				}
			}
		}
		if out.NextPageToken == "" {
			if found == nil {
				return observation(r), ErrDeliveryUnknown
			}
			return a.bindTask(ctx, r, found)
		}
		if token == out.NextPageToken {
			return observation(r), ErrIdentity
		}
		token = out.NextPageToken
	}
	return observation(r), ErrDeliveryUnknown
}
func (a *Adapter) bindTask(ctx context.Context, r rt.EngineAttempt, t *a2a.Task) (rt.Observation, error) {
	if t.GetId() == "" || t.GetContextId() != r.ContextID || r.TaskID != "" && t.Id != r.TaskID {
		return observation(r), ErrIdentity
	}
	if r.TaskID == "" {
		matched := false
		for _, m := range t.History {
			if m.GetMessageId() == r.MessageID && m.GetRole() == a2a.Role_ROLE_USER {
				matched = true
			}
		}
		if !matched {
			return observation(r), ErrDeliveryUnknown
		}
	}
	if r.AnswerPhase == "sending" {
		found := false
		for _, message := range t.History {
			if message.GetMessageId() != r.AnswerMessageID {
				continue
			}
			if message.GetRole() != a2a.Role_ROLE_USER || len(message.GetParts()) != 1 ||
				fmt.Sprintf("%x", sha256.Sum256([]byte(message.GetParts()[0].GetText()))) != r.AnswerDigest {
				return observation(r), ErrIdentity
			}
			found = true
		}
		if !found {
			return observation(r), ErrDeliveryUnknown
		}
	}
	state, stateErr := taskState(t.GetStatus().GetState())
	if stateErr != nil && !errors.Is(stateErr, ErrInteractionRequired) {
		return observation(r), stateErr
	}
	r.TaskID = t.Id
	r.Phase = "active"
	r.State = state
	var question *rt.Question
	if t.GetStatus().GetState() == a2a.TaskState_TASK_STATE_INPUT_REQUIRED {
		question, stateErr = pendingQuestion(t.GetStatus().GetMessage())
		if stateErr == nil {
			if r.QuestionID != "" && r.QuestionID != question.ID {
				return observation(r), ErrInteractionRequired
			}
			r.QuestionID = question.ID
		}
	}
	if r.AnswerPhase == "sending" {
		for _, message := range t.History {
			if message.GetMessageId() == r.AnswerMessageID && message.GetRole() == a2a.Role_ROLE_USER {
				r.AnswerPhase = "sent"
				break
			}
		}
	}
	if t.GetStatus().GetState() == a2a.TaskState_TASK_STATE_CANCELED {
		r.CancelRequested = true
	}
	r, err := a.save(ctx, r)
	if err == nil && r.CancelRequested {
		if err = a.delete(ctx, r); err != nil {
			return observation(r), err
		}
		r, err = a.journal.GetEngineAttempt(ctx, r.AttemptID)
	}
	result := observation(r)
	result.EngineStatus = t.GetStatus().GetState().String()
	if r.AnswerPhase == "" {
		result.Question = question
	}
	return result, errors.Join(err, stateErr)
}

func pendingQuestion(message *a2a.Message) (*rt.Question, error) {
	if message == nil {
		return nil, ErrInteractionRequired
	}
	decoded, err := pbconv.FromProtoMessage(message)
	if err != nil {
		return nil, ErrInteractionRequired
	}
	request, err := kagenta2a.ParseAskUserRequest(decoded)
	if err != nil || request == nil || len(request.Questions) != 1 || request.Nested != nil ||
		request.ID == "" || len(request.ID) > 160 || len(request.Questions[0].Question) == 0 || len(request.Questions[0].Question) > 2000 ||
		request.Questions[0].Multiple {
		return nil, ErrInteractionRequired
	}
	return &rt.Question{ID: request.ID, Text: request.Questions[0].Question, Choices: request.Questions[0].Choices}, nil
}

// AnswerQuestion sends one exact answer after durable send intent. An unknown
// response is inspected by its message ID and never retried as a new send.
func (a *Adapter) AnswerQuestion(ctx context.Context, handle, questionID, answer string) (rt.Observation, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	s, r, err := a.load(ctx, handle)
	if err != nil {
		return rt.Observation{}, err
	}
	answer = strings.TrimSpace(answer)
	if r.TaskID == "" || r.QuestionID == "" || r.QuestionID != questionID || r.CancelRequested || r.DeleteRequested ||
		r.Deleted || r.State.Terminal() || answer == "" || len(answer) > 2000 {
		return observation(r), rt.ErrConflict
	}
	if r.AnswerPhase == "sent" {
		return observation(r), rt.ErrConflict
	}
	answerDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(answer)))
	if r.AnswerPhase == "sending" && r.AnswerDigest != answerDigest {
		return observation(r), rt.ErrConflict
	}
	task, err := a.tasks.GetTask(route(ctx, r), &a2a.GetTaskRequest{Id: r.TaskID}, grpc.MaxCallRecvMsgSize(artifacts.MaxBytes))
	if err != nil {
		return observation(r), rpcError("inspect pending question", err)
	}
	if task.GetId() != r.TaskID || task.GetContextId() != r.ContextID {
		return observation(r), ErrIdentity
	}
	if r.AnswerPhase != "" {
		return a.bindTask(ctx, r, task)
	}
	if task.GetStatus().GetState() != a2a.TaskState_TASK_STATE_INPUT_REQUIRED {
		return observation(r), rt.ErrConflict
	}
	question, err := pendingQuestion(task.GetStatus().GetMessage())
	if err != nil || question.ID != questionID {
		return observation(r), rt.ErrConflict
	}
	if len(question.Choices) > 0 {
		found := false
		for _, choice := range question.Choices {
			found = found || answer == choice
		}
		if !found {
			return observation(r), rt.ErrInvalidInput
		}
	}
	if err := a.admit(ctx, s); err != nil {
		return observation(r), err
	}
	r.AnswerMessageID = "marina-answer-" + r.AttemptID
	r.AnswerDigest = answerDigest
	r.AnswerPhase = "sending"
	r, err = a.save(ctx, r)
	if err != nil {
		return observation(r), err
	}
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(answer))
	message.ID, message.TaskID, message.ContextID = r.AnswerMessageID, a2atype.TaskID(r.TaskID), r.ContextID
	if err := kagenta2a.AttachHITL(message, kagenta2a.AskUserResponse{Type: kagenta2a.HITLTypeAskUserResponse,
		ID: questionID, Answers: []kagenta2a.AskUserAnswer{{Answer: []string{answer}}}}); err != nil {
		return observation(r), err
	}
	request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: message})
	if err != nil {
		return observation(r), err
	}
	response, err := a.tasks.SendMessage(route(ctx, r), request, grpc.MaxCallRecvMsgSize(artifacts.MaxBytes))
	if err != nil {
		return observation(r), rpcError("answer question (delivery uncertain)", err)
	}
	r.AnswerPhase = "sent"
	r, err = a.save(ctx, r)
	if err != nil {
		return observation(r), err
	}
	if task := response.GetTask(); task != nil {
		return a.bindTask(ctx, r, task)
	}
	return observation(r), nil
}
func taskState(s a2a.TaskState) (rt.State, error) {
	switch s {
	case a2a.TaskState_TASK_STATE_SUBMITTED, a2a.TaskState_TASK_STATE_WORKING:
		return rt.StateRunning, nil
	case a2a.TaskState_TASK_STATE_COMPLETED:
		return rt.StateSucceeded, nil
	case a2a.TaskState_TASK_STATE_FAILED, a2a.TaskState_TASK_STATE_REJECTED:
		return rt.StateFailed, nil
	case a2a.TaskState_TASK_STATE_CANCELED:
		return rt.StateRunning, nil // deletion still required to prove termination
	case a2a.TaskState_TASK_STATE_INPUT_REQUIRED, a2a.TaskState_TASK_STATE_AUTH_REQUIRED:
		return rt.StateRunning, ErrInteractionRequired
	default:
		return "", errors.New("kagent returned unknown task state")
	}
}

func (a *Adapter) Cancel(ctx context.Context, handle string, _ bool) (rt.Observation, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	s, r, err := a.load(ctx, handle)
	if err != nil {
		return rt.Observation{}, err
	}
	if !r.CancelRequested {
		r.CancelRequested = true
		r, err = a.save(ctx, r)
		if err != nil {
			return observation(r), err
		}
	}
	return a.cancel(ctx, s, r)
}
func (a *Adapter) cancel(ctx context.Context, s rt.Specification, r rt.EngineAttempt) (rt.Observation, error) {
	if r.Deleted {
		return observation(r), nil
	}
	if r.InstanceID == "" {
		// Recover an uncertain create with its exact key even after the deadline,
		// solely to terminate it. No message may be sent from this branch.
		out, err := a.instances.CreateAgentInstance(ctx, &api.CreateAgentInstanceRequest{Harness: &api.ResourceReference{Namespace: s.Engine.Namespace, Name: s.Engine.Harness}, AgentTemplate: &api.ResourceReference{Namespace: s.Engine.Namespace, Name: s.Engine.Template}, RequestId: s.IdempotencyKey})
		if err != nil {
			return observation(r), rpcError("recover instance for cancellation", err)
		}
		i := out.GetAgentInstance()
		if i.GetId() == "" {
			return observation(r), ErrIdentity
		}
		r.InstanceID = i.Id
		r.ContextID = i.ContextId
		r.Route = i.A2AAuthority
		r.Revision = i.PreparedRevision
		r.Phase = "ready"
		r, err = a.save(ctx, r)
		if err != nil {
			return observation(r), err
		}
	}
	// Delete is the pinned synchronous suspend/delete lifecycle, not an A2A
	// status assertion. Do not report cancelled until a subsequent Get is absent.
	if err := a.delete(ctx, r); err != nil {
		return observation(r), err
	}
	r, err := a.journal.GetEngineAttempt(ctx, r.AttemptID)
	return observation(r), err
}
func (a *Adapter) delete(ctx context.Context, r rt.EngineAttempt) error {
	if r.Deleted {
		return nil
	}
	if !r.DeleteRequested {
		r.DeleteRequested = true
		var err error
		r, err = a.save(ctx, r)
		if err != nil {
			return err
		}
	}
	current, err := a.instances.GetAgentInstance(ctx, &api.GetAgentInstanceRequest{AgentInstanceId: r.InstanceID})
	if err == nil {
		if !matchesRecord(current.GetAgentInstance(), r) {
			return ErrIdentity
		}
		_, err = a.instances.DeleteAgentInstance(ctx, &api.DeleteAgentInstanceRequest{AgentInstanceId: r.InstanceID})
		if err != nil && status.Code(err) != codes.NotFound {
			return rpcError("delete instance", err)
		}
		_, err = a.instances.GetAgentInstance(ctx, &api.GetAgentInstanceRequest{AgentInstanceId: r.InstanceID})
	}
	if status.Code(err) != codes.NotFound {
		if err != nil {
			return rpcError("observe deletion", err)
		}
		return errors.New("kagent instance termination not yet observed")
	}
	r.Deleted = true
	if r.CancelRequested && !r.State.Terminal() {
		r.State = rt.StateCancelled
	}
	_, err = a.save(ctx, r)
	return err
}

func (a *Adapter) CollectArtifacts(ctx context.Context, handle string) ([]rt.Artifact, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	s, r, err := a.load(ctx, handle)
	if err != nil {
		return nil, err
	}
	if !r.State.Terminal() {
		return nil, rt.ErrConflict
	}
	if !r.Collected {
		if !r.Deleted {
			task, err := a.tasks.GetTask(route(ctx, r), &a2a.GetTaskRequest{Id: r.TaskID}, grpc.MaxCallRecvMsgSize(artifacts.MaxBytes))
			if err != nil {
				return nil, rpcError("collect artifacts", err)
			}
			state, err := taskState(task.GetStatus().GetState())
			if err != nil || state != r.State || task.Id != r.TaskID || task.ContextId != r.ContextID {
				return nil, ErrIdentity
			}
			for index, output := range task.Artifacts {
				// Store the bounded A2A envelope itself. Never fetch an engine-supplied URL
				// or treat these bytes as independently verified workflow evidence.
				data, err := protojson.Marshal(output)
				if err != nil {
					return nil, err
				}
				digest, err := a.blobs.Put(data)
				if err != nil {
					return nil, err
				}
				_, _, err = a.journal.RecordRuntimeArtifact(ctx, rt.Artifact{ID: fmt.Sprintf("%s-kagent-%d", s.AttemptID, index), ExecutionID: s.ExecutionID, RuntimeAttemptID: s.AttemptID, Name: fmt.Sprintf("kagent-output-%d.json", index), Kind: "kagent-a2a-output", URI: "artifact://" + digest, Digest: digest, SizeBytes: int64(len(data))})
				if err != nil {
					return nil, err
				}
			}
		} else if !r.CancelRequested {
			return nil, ErrIdentity
		}
		r.Collected = true
		if _, err = a.save(ctx, r); err != nil {
			return nil, err
		}
	}
	all, err := a.journal.ListRuntimeArtifacts(ctx, s.ExecutionID)
	if err != nil {
		return nil, err
	}
	var out []rt.Artifact
	for _, item := range all {
		if item.RuntimeAttemptID == s.AttemptID {
			out = append(out, item)
		}
	}
	return out, nil
}
func (a *Adapter) Cleanup(ctx context.Context, handle string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	_, r, err := a.load(ctx, handle)
	if err != nil {
		return err
	}
	if !r.State.Terminal() || !r.Collected {
		return rt.ErrConflict
	}
	return a.delete(ctx, r)
}
