package verification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var ErrInvalidInput = errors.New("invalid verification input")

type Outcome string

const (
	OutcomePassed Outcome = "passed"
	OutcomeFailed Outcome = "failed"
)

type Check struct {
	ID               string
	Kind             string
	WorkingDirectory string
	SourceRevision   string
	Command          []string
}

type CheckResult struct {
	ID             string
	Kind           string
	SourceRevision string
	Command        []string
	CommandDigest  string
	Outcome        Outcome
	ExitCode       int
	Output         string
	OutputDigest   string
	ObservedAt     time.Time
	Duration       time.Duration
}

const repositoryChecksVersion = "marina-checks/v1"

type repositoryChecksManifest struct {
	Version string            `json:"version"`
	Checks  []repositoryCheck `json:"checks"`
}

type repositoryCheck struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Command []string `json:"command"`
}

// LoadRepositoryChecks reads the repository-owned deterministic check list.
// Commands remain argument vectors and can never opt into a command shell.
func LoadRepositoryChecks(repository string, sourceRevision string) ([]Check, error) {
	repository = strings.TrimSpace(repository)
	sourceRevision = strings.ToLower(strings.TrimSpace(sourceRevision))
	if repository == "" || !fullRevision(sourceRevision) {
		return nil, ErrInvalidInput
	}
	absolute, err := filepath.Abs(repository)
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(absolute, ".marina", "checks.json")
	file, err := os.Open(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("open repository checks: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 256<<10))
	decoder.DisallowUnknownFields()
	var manifest repositoryChecksManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode repository checks: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF || manifest.Version != repositoryChecksVersion ||
		len(manifest.Checks) == 0 || len(manifest.Checks) > 64 {
		return nil, ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(manifest.Checks))
	checks := make([]Check, 0, len(manifest.Checks))
	for _, configured := range manifest.Checks {
		configured.ID = strings.TrimSpace(configured.ID)
		configured.Kind = strings.TrimSpace(configured.Kind)
		if configured.ID == "" || configured.Kind == "" || len(configured.Command) == 0 || isShell(configured.Command[0]) {
			return nil, ErrInvalidInput
		}
		if _, duplicate := seen[configured.ID]; duplicate {
			return nil, ErrInvalidInput
		}
		seen[configured.ID] = struct{}{}
		for _, argument := range configured.Command {
			if strings.TrimSpace(argument) == "" {
				return nil, ErrInvalidInput
			}
		}
		checks = append(checks, Check{
			ID: configured.ID, Kind: configured.Kind, WorkingDirectory: absolute,
			SourceRevision: sourceRevision, Command: slices.Clone(configured.Command),
		})
	}
	return checks, nil
}

type Runner struct {
	gitBinary string
	timeout   time.Duration
	maxOutput int
	clock     func() time.Time
}

func NewRunner(gitBinary string, timeout time.Duration, maxOutput int) (*Runner, error) {
	if strings.TrimSpace(gitBinary) == "" || timeout <= 0 || maxOutput < 1 {
		return nil, ErrInvalidInput
	}
	return &Runner{gitBinary: strings.TrimSpace(gitBinary), timeout: timeout, maxOutput: maxOutput, clock: time.Now}, nil
}

func (runner *Runner) Run(ctx context.Context, check Check) (CheckResult, error) {
	check.ID = strings.TrimSpace(check.ID)
	check.Kind = strings.TrimSpace(check.Kind)
	check.WorkingDirectory = strings.TrimSpace(check.WorkingDirectory)
	check.SourceRevision = strings.ToLower(strings.TrimSpace(check.SourceRevision))
	if check.ID == "" || check.Kind == "" || check.WorkingDirectory == "" || len(check.Command) == 0 ||
		!fullRevision(check.SourceRevision) || strings.TrimSpace(check.Command[0]) == "" || isShell(check.Command[0]) {
		return CheckResult{}, ErrInvalidInput
	}
	absolute, err := filepath.Abs(check.WorkingDirectory)
	if err != nil {
		return CheckResult{}, err
	}
	observedHead, err := runner.gitHead(ctx, absolute)
	if err != nil {
		return CheckResult{}, err
	}
	if observedHead != check.SourceRevision {
		return CheckResult{}, fmt.Errorf("working tree is at %s, expected %s: %w", observedHead, check.SourceRevision, ErrInvalidInput)
	}
	encodedCommand, _ := json.Marshal(check.Command)
	commandHash := sha256.Sum256(encodedCommand)
	started := runner.clock().UTC()
	operationCtx, cancel := context.WithTimeout(ctx, runner.timeout)
	defer cancel()
	command := exec.CommandContext(operationCtx, check.Command[0], check.Command[1:]...)
	command.Dir = absolute
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"LANG=C",
	}
	output := &cappedBuffer{limit: runner.maxOutput}
	command.Stdout = output
	command.Stderr = output
	err = command.Run()
	finished := runner.clock().UTC()
	if operationCtx.Err() != nil {
		return CheckResult{}, operationCtx.Err()
	}
	exitCode := 0
	outcome := OutcomePassed
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return CheckResult{}, fmt.Errorf("execute verification check: %w", err)
		}
		exitCode = exitErr.ExitCode()
		outcome = OutcomeFailed
	}
	outputHash := sha256.Sum256(output.Bytes())
	return CheckResult{
		ID: check.ID, Kind: check.Kind, SourceRevision: check.SourceRevision,
		Command: slices.Clone(check.Command), CommandDigest: "sha256:" + hex.EncodeToString(commandHash[:]),
		Outcome: outcome, ExitCode: exitCode, Output: output.String(),
		OutputDigest: "sha256:" + hex.EncodeToString(outputHash[:]), ObservedAt: finished,
		Duration: finished.Sub(started),
	}, nil
}

func (runner *Runner) gitHead(ctx context.Context, directory string) (string, error) {
	command := exec.CommandContext(ctx, runner.gitBinary, "rev-parse", "HEAD")
	command.Dir = directory
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"LANG=C",
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect verification source revision: %w: %s", err, output)
	}
	return strings.ToLower(strings.TrimSpace(string(output))), nil
}

func fullRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func isShell(executable string) bool {
	switch filepath.Base(executable) {
	case "sh", "bash", "zsh", "fish", "dash", "cmd", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (writer *cappedBuffer) Write(payload []byte) (int, error) {
	original := len(payload)
	remaining := writer.limit - writer.buffer.Len()
	if remaining > 0 {
		if len(payload) > remaining {
			payload = payload[:remaining]
		}
		_, _ = writer.buffer.Write(payload)
	}
	return original, nil
}

func (writer *cappedBuffer) Bytes() []byte  { return writer.buffer.Bytes() }
func (writer *cappedBuffer) String() string { return writer.buffer.String() }

type EvidenceStatus string

const (
	EvidenceCurrent    EvidenceStatus = "current"
	EvidenceStale      EvidenceStatus = "stale"
	EvidenceRejected   EvidenceStatus = "rejected"
	EvidenceSuperseded EvidenceStatus = "superseded"
)

type Evidence struct {
	ID                    string
	Gate                  string
	ProducerID            string
	VerifierID            string
	ProducerContextDigest string
	VerifierContextDigest string
	SourceRevision        string
	ContextDigest         string
	PlanRevisionID        string
	PolicyVersion         string
	CommandDigest         string
	Outcome               Outcome
	ObservedAt            time.Time
	ExpiresAt             time.Time
}

type Subject struct {
	SourceRevision string
	ContextDigest  string
	PlanRevisionID string
	PolicyVersion  string
	CommandDigests map[string]string
}

type EvidenceEvaluation struct {
	Evidence Evidence
	Status   EvidenceStatus
	Reason   string
}

func EvaluateEvidence(now time.Time, evidence Evidence, subject Subject, requireIndependent bool) EvidenceEvaluation {
	result := EvidenceEvaluation{Evidence: evidence, Status: EvidenceRejected}
	switch {
	case strings.TrimSpace(evidence.ID) == "" || strings.TrimSpace(evidence.Gate) == "":
		result.Reason = "evidence identity and gate are required"
	case evidence.SourceRevision != subject.SourceRevision || evidence.ContextDigest != subject.ContextDigest ||
		evidence.PlanRevisionID != subject.PlanRevisionID || evidence.PolicyVersion != subject.PolicyVersion:
		result.Status = EvidenceSuperseded
		result.Reason = "evidence is bound to a superseded source, context, plan, or policy"
	case evidence.CommandDigest != subject.CommandDigests[evidence.Gate]:
		result.Status = EvidenceStale
		result.Reason = "verification command changed"
	case evidence.ExpiresAt.IsZero() || !now.Before(evidence.ExpiresAt):
		result.Status = EvidenceStale
		result.Reason = "evidence observation window expired"
	case evidence.Outcome != OutcomePassed:
		result.Reason = "evidence outcome did not pass"
	case requireIndependent && (evidence.ProducerID == evidence.VerifierID ||
		evidence.ProducerContextDigest == evidence.VerifierContextDigest ||
		evidence.ProducerID == "" || evidence.VerifierID == ""):
		result.Reason = "consequential evidence lacks an independent verifier"
	default:
		result.Status = EvidenceCurrent
		result.Reason = "evidence is current for the exact subject"
	}
	return result
}

func PlanInvalidatedGates(now time.Time, records []Evidence, subject Subject, requireIndependent bool) []string {
	known := make(map[string]bool, len(subject.CommandDigests))
	for gate := range subject.CommandDigests {
		if strings.TrimSpace(gate) != "" {
			known[gate] = false
		}
	}
	for _, record := range records {
		if strings.TrimSpace(record.Gate) == "" {
			continue
		}
		if _, exists := known[record.Gate]; !exists {
			known[record.Gate] = false
		}
		if EvaluateEvidence(now, record, subject, requireIndependent).Status == EvidenceCurrent {
			known[record.Gate] = true
		}
	}
	invalidated := make([]string, 0)
	for gate, current := range known {
		if !current {
			invalidated = append(invalidated, gate)
		}
	}
	slices.Sort(invalidated)
	return invalidated
}

func RequiresIndependentReview(riskClass string) bool {
	switch strings.ToLower(strings.TrimSpace(riskClass)) {
	case "high", "critical":
		return true
	default:
		return false
	}
}
