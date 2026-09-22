package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

func analysisRequested(task workcontrol.WorkItem) bool {
	for _, grant := range task.Budget.Authority {
		if strings.HasPrefix(grant, "analysis.") {
			return true
		}
	}
	return false
}

func (c *Controller) prepareAnalysis(ctx context.Context, e execution.Execution, r workcontrol.Run, task workcontrol.WorkItem, s state) error {
	p := c.Config.Analysis
	if p == nil || p.Validate() != nil || len(task.Budget.Authority) != 1 || task.Budget.Authority[0] != c.Config.AnalysisAuthority() {
		return c.block(ctx, e, r, "Task must authorize the exact installed analysis profile")
	}
	if strings.TrimSpace(task.AcceptanceCriteria) == "" || task.Budget.MaxTokens < 1 || task.Budget.MaxTokens > p.MaxTokens || task.Budget.TimeMinutes < 1 || task.Budget.TimeMinutes > p.BudgetMinutes || e.MaxAttempts < 1 {
		return c.block(ctx, e, r, "Analysis requires acceptance criteria and positive task budgets within the profile limits")
	}
	if task.Budget.MaxCostMinor > 0 && (task.Budget.Currency != c.Config.Model.Currency || c.Config.Model.InputMicrosPerMillion == 0 || c.Config.Model.OutputMicrosPerMillion == 0) {
		return c.block(ctx, e, r, "Task cost currency must match configured nonzero model price rates")
	}
	if p.HarnessVersion == analysis.Harness && task.Context != nil && (len(task.Context.Sources) > 0 || len(task.Context.Messages) > 0) {
		return c.block(ctx, e, r, "Selected project context requires the installed langchain-create-agent/v2 harness; authorize a new task after upgrading the profile")
	}
	s.Analysis = p
	s.ProfileDigest = c.Config.AnalysisDigest()
	s.Base = p.Fixture.InputDigest()
	s.Head = s.Base
	s.Branch = ""
	s.Step = "plan"
	return c.save(ctx, e, s)
}

func (c *Controller) advanceAnalysisResult(ctx context.Context, e execution.Execution, r workcontrol.Run, s state) error {
	switch s.Step {
	case "analysis_verify":
		b, err := c.Artifacts.Get(s.ImplementationDigest)
		if err != nil {
			return c.block(ctx, e, r, "Analysis result artifact is missing or invalid")
		}
		var result worker.Result
		if json.Unmarshal(b, &result) != nil || result.Analysis == nil || result.ProfileDigest != s.ProfileDigest || result.InputDigest != s.Base || result.SourceRevision != "" || result.Stage != "analyze" || len(result.Changes) > 0 || len(result.Checks) > 0 {
			return c.block(ctx, e, r, "Analysis report does not match its frozen input and profile")
		}
		if err = analysis.ValidateReport(s.Analysis.Fixture, *result.Analysis); err != nil {
			return c.block(ctx, e, r, "Independent analysis validation failed: "+err.Error())
		}
		if r.Status != workcontrol.RunVerifying {
			if err = c.Provider.BeginVerification(ctx, r.ID, guard(e, r, "verify")); err != nil {
				return err
			}
		}
		if e.State != execution.StateVerifying {
			if err = c.transition(ctx, e, execution.StateVerifying, "Validating the diagnostic report against its frozen fixture"); err != nil {
				return err
			}
		}
		validation := analysis.Validation{ContextDigest: s.ContextDigest, Version: 1, Validator: "marina:diagnostic-validator/v1", ProfileDigest: s.ProfileDigest, InputDigest: s.Base, OutputDigest: s.ImplementationDigest, Report: *result.Analysis, Sources: s.Analysis.Fixture.Sources, Checks: []string{"required findings", "no duplicate or invented findings", "source IDs and verbatim causal quotes", "immutable input/profile/output binding"}}
		encoded, _ := json.Marshal(validation)
		digest, err := c.Artifacts.Put(encoded)
		if err != nil {
			return err
		}
		s.VerificationDigest = digest
		s.Verifier = validation.Validator
		s.ValidationObservedAt = time.Now().UTC()
		s.Step = "analysis_evidence"
		return c.save(ctx, e, s)
	case "analysis_evidence":
		// Replay this independent validation after restart and before completion.
		if err := c.checkAnalysisEvidence(s); err != nil {
			return c.block(ctx, e, r, err.Error())
		}
		id := e.ID + ":analysis-validation"
		_, err := c.Provider.RecordEvidence(ctx, r.ID, guard(e, r, "analysis-evidence"), workcontrol.EvidenceSubmission{ID: id, PlanRevisionID: s.PlanID, Type: "test", Producer: s.Implementer, Verifier: s.Verifier, SubjectDigest: s.ImplementationDigest, SourceRevision: s.Base, Command: "marina diagnostic-validator/v1", Outcome: "passed", ProvenanceURI: c.artifactURL(s.VerificationDigest), ProvenanceDigest: s.VerificationDigest, ObservedAt: s.ValidationObservedAt, ExpiresAt: s.ValidationObservedAt.Add(24 * time.Hour), ArtifactURI: c.artifactURL(s.VerificationDigest), ArtifactDigest: s.VerificationDigest, PolicyVersion: c.Config.PolicyVersion})
		if err != nil {
			return err
		}
		e, err = c.Journal.GetExecution(ctx, e.ID)
		if err != nil {
			return err
		}
		_, _, _, err = c.Journal.AddEvidence(ctx, e.ID, e.Version, execution.EvidenceInput{Key: id, Kind: "test", SourceRevision: s.Base, Environment: "Independent controller validator " + s.Verifier, Command: "marina diagnostic-validator/v1", Summary: "passed", URI: c.artifactURL(s.VerificationDigest), ObservedAt: s.ValidationObservedAt})
		if err != nil {
			return err
		}
		s.EvidenceIDs = []string{id}
		s.Step = "complete"
		return c.save(ctx, e, s)
	case "complete":
		if err := c.checkAnalysisEvidence(s); err != nil {
			return c.block(ctx, e, r, err.Error())
		}
		if len(s.EvidenceIDs) != 1 {
			return c.block(ctx, e, r, "Analysis completion requires independently recorded validation evidence")
		}
		if err := c.Provider.CompleteRun(ctx, r.ID, guard(e, r, "complete"), "Validated diagnostic analysis: "+c.artifactURL(s.VerificationDigest), s.EvidenceIDs); err != nil {
			return err
		}
		return c.transition(ctx, e, execution.StateCompleted, "Validated diagnostic analysis available")
	default:
		return fmt.Errorf("unsupported analysis workflow step %q", s.Step)
	}
}

func (c *Controller) checkAnalysisEvidence(s state) error {
	invalid := errors.New("Independent analysis evidence is missing or does not match the frozen inputs/profile/output")
	b, err := c.Artifacts.Get(s.VerificationDigest)
	if err != nil {
		return invalid
	}
	var proof analysis.Validation
	if json.Unmarshal(b, &proof) != nil || proof.Version != 1 || proof.Validator != "marina:diagnostic-validator/v1" || proof.ProfileDigest != s.ProfileDigest || (proof.ContextDigest != "" && proof.ContextDigest != s.ContextDigest) || proof.InputDigest != s.Base || proof.OutputDigest != s.ImplementationDigest || analysis.Digest(proof.Sources) != s.Base {
		return invalid
	}
	if s.Analysis.HarnessVersion == analysis.HarnessWithContext && (proof.ContextDigest == "" || proof.ContextDigest != s.ContextDigest) {
		return invalid
	}
	b, err = c.Artifacts.Get(s.ImplementationDigest)
	if err != nil {
		return invalid
	}
	var output worker.Result
	if json.Unmarshal(b, &output) != nil || output.Analysis == nil || output.ProfileDigest != s.ProfileDigest || output.InputDigest != s.Base || analysis.Digest(*output.Analysis) != analysis.Digest(proof.Report) {
		return invalid
	}
	if analysis.ValidateReport(s.Analysis.Fixture, proof.Report) != nil {
		return invalid
	}
	return nil
}
