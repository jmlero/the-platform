package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jmlero/the-platform/marina/internal/execution"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

func (c *Controller) candidate(s state) (workresult.Candidate, error) {
	data, err := c.Artifacts.Get(s.CandidateDigest)
	if err != nil {
		return workresult.Candidate{}, err
	}
	var candidate workresult.Candidate
	if err = json.Unmarshal(data, &candidate); err != nil {
		return candidate, err
	}
	// Revalidate the producer's immutable result as well as the candidate copy.
	result, err := c.checkedResult(s.ImplementationDigest, workresult.Expected{Stage: "implement", SourceRevision: candidate.BaseRevision, AllowedPaths: c.Config.AllowedPaths})
	if err != nil || candidate.Digest() != (workresult.Candidate{BaseRevision: result.SourceRevision, Changes: result.Changes}).Digest() {
		return candidate, errors.New("candidate differs from the saved implementation")
	}
	return candidate, nil
}

func (c *Controller) reviewInput(ctx context.Context, e execution.Execution, task workcontrol.WorkItem, s state) (worker.Input, error) {
	input, err := c.engineInput(ctx, task, s)
	if err != nil {
		return input, err
	}
	candidate, err := c.candidate(s)
	if err != nil || candidate.BaseRevision != s.Head {
		return input, fmt.Errorf("review candidate is unavailable or on a different base: %w", errInvalidWorkerResult)
	}
	if err := validatePRDCandidate(task, candidate); err != nil {
		return input, fmt.Errorf("%w: %v", errInvalidWorkerResult, err)
	}
	source, err := c.Source(e)
	if err != nil {
		return input, err
	}
	reader, ok := source.(sourcecontrol.ReviewSource)
	if !ok {
		return input, fmt.Errorf("source cannot supply pinned original files for review: %w", errInvalidWorkerResult)
	}
	input.Stage, input.SourceURL, input.Candidate = "review", "", &candidate
	input.CandidateDigest, input.ProfileDigest = candidate.Digest(), s.ConfigDigest
	for _, change := range candidate.Changes {
		content, exists, err := reader.ReadReviewFile(ctx, c.Config.Git.Repository, candidate.BaseRevision, change.Path)
		if err != nil {
			return input, err
		}
		if change.Delete && !exists {
			return input, fmt.Errorf("candidate deletes a missing file: %w", errInvalidWorkerResult)
		}
		before := workresult.BaseFile{Path: change.Path, Exists: exists, Content: content}
		if utf8.Valid(content) && !strings.ContainsRune(string(content), '\x00') {
			text := string(content)
			before.Text, before.Content = &text, nil
		}
		input.Before = append(input.Before, before)
	}
	for i := range candidate.Changes {
		change := &candidate.Changes[i]
		if !change.Delete && utf8.Valid(change.Content) && !strings.ContainsRune(string(change.Content), '\x00') {
			text := string(change.Content)
			change.Text, change.Content = &text, nil
		}
	}
	if len(s.Failures) > 0 {
		input.Feedback, err = c.repairFeedback(s)
		if err != nil {
			return input, err
		}
		input.Repair, err = c.savedRepairPlan(s)
		if err != nil {
			return input, err
		}
	}
	return input, nil
}

func (c *Controller) checkedReview(ctx context.Context, e execution.Execution, s state) (workresult.Candidate, workresult.Result, error) {
	candidate, err := c.candidate(s)
	if err != nil {
		return candidate, workresult.Result{}, err
	}
	if err = c.Config.ValidateReviewer(); err != nil {
		return candidate, workresult.Result{}, err
	}
	attempt, err := c.Journal.GetRuntimeAttempt(ctx, s.Reviewer)
	if err != nil {
		return candidate, workresult.Result{}, err
	}
	spec := attempt.Specification
	if attempt.State != rt.StateSucceeded || attempt.ExecutionID != e.ID || s.Reviewer == s.Implementer || spec.Stage != "review" || spec.Engine == nil ||
		spec.Engine.Harness != workresult.ReviewerHarness || spec.ProfileDigest != s.ConfigDigest || spec.SourceRevision != candidate.BaseRevision {
		return candidate, workresult.Result{}, errors.New("review does not belong to an independent frozen attempt")
	}
	result, err := c.checkedResult(s.ReviewDigest, workresult.Expected{Stage: "review", SourceRevision: candidate.BaseRevision,
		InputDigest: spec.InputDigest, ProfileDigest: spec.ProfileDigest, PatchDigest: candidate.Digest(),
		NotBefore: attempt.CreatedAt.Add(-time.Minute), Now: time.Now()})
	return candidate, result, err
}

func (c *Controller) publicationCandidate(ctx context.Context, e execution.Execution, s state) (workresult.Candidate, error) {
	candidate, result, err := c.checkedReview(ctx, e, s)
	if err != nil || candidate.BaseRevision != s.Head || result.Review == nil || !result.Review.Passed() {
		return candidate, errors.New("commit requires a passing independent review of this exact patch and base")
	}
	totals, err := c.Journal.ExecutionModelTotals(ctx, e.ID)
	if err != nil {
		return candidate, err
	}
	if totals.PendingCalls != 0 {
		return candidate, errors.New("review usage is uncertain; reservations are retained and publication is denied")
	}
	return candidate, nil
}

func (c *Controller) advanceReview(ctx context.Context, e execution.Execution, r workcontrol.Run, task workcontrol.WorkItem, s state) error {
	if s.Step == "review_evidence" || s.Step == "review_rejected" {
		return c.reviewEvidence(ctx, e, r, s)
	}
	if err := c.Config.ValidateReviewer(); err != nil {
		return c.block(ctx, e, r, err.Error())
	}
	if s.CandidateDigest == "" {
		result, err := c.checkedResult(s.ImplementationDigest, workresult.Expected{Stage: "implement", SourceRevision: s.Head, AllowedPaths: c.Config.AllowedPaths})
		if err != nil {
			return c.block(ctx, e, r, "Review cannot admit the saved implementation artifact")
		}
		data, err := json.Marshal(workresult.Candidate{BaseRevision: s.Head, Changes: result.Changes})
		if err != nil {
			return err
		}
		s.CandidateDigest, err = c.Artifacts.Put(data)
		if err != nil {
			return err
		}
		return c.save(ctx, e, s)
	}
	attempt, done, result, digest, err := c.worker(ctx, e, task, s, "review")
	if errors.Is(err, execution.ErrBudgetExhausted) || errors.Is(err, errInvalidWorkerResult) || errors.Is(err, errEngineQuestionStopped) {
		return c.block(ctx, e, r, "Independent review unavailable; nothing was committed: "+err.Error())
	}
	if err != nil || !done {
		return err
	}
	if result.Review == nil {
		reason := "Independent review produced no valid verdict; nothing was committed"
		if denial, err := c.Journal.ModelBudgetDenial(ctx, e.ID, attempt.ID); err != nil {
			return err
		} else if denial != "" {
			reason += ": " + denial
		}
		return c.block(ctx, e, r, reason)
	}
	s.Reviewer, s.ReviewDigest = attempt.ID, digest
	s.EngineStatus = ""
	if result.Review.Passed() {
		s.Step = "publish"
		if s.Head == s.baseRevision() {
			s.Step = "branch"
		}
	} else {
		s.Step = "review_rejected"
	}
	return c.save(ctx, e, s)
}

func (c *Controller) reviewEvidence(ctx context.Context, e execution.Execution, r workcontrol.Run, s state) error {
	candidate, result, err := c.checkedReview(ctx, e, s)
	if err != nil {
		return c.block(ctx, e, r, "Independent review evidence is missing or invalid")
	}
	outcome := "failed"
	commit := ""
	if result.Review.Passed() {
		outcome, commit = "passed", s.Head
		if s.Step != "review_evidence" || commit == candidate.BaseRevision {
			return c.block(ctx, e, r, "Review evidence has no resulting commit")
		}
	} else if s.Step != "review_rejected" || s.Head != candidate.BaseRevision {
		return c.block(ctx, e, r, "Rejected review cannot authorize a commit")
	}
	// The model cannot supply its identity, profile, or resulting commit.
	data, err := json.Marshal(struct {
		CandidateDigest, BaseRevision, Commit, Reviewer, Model, ProfileDigest string
		Verdict                                                               workresult.Result
	}{candidate.Digest(), candidate.BaseRevision, commit, s.Reviewer, c.Config.ReviewerModel.Name, s.ConfigDigest, result})
	if err != nil {
		return err
	}
	digest, err := c.Artifacts.Put(data)
	if err != nil {
		return err
	}
	id := operationKey(e, s, "review-evidence")
	if s.ReviewEvidenceID == "" {
		command := "independent semantic review: " + c.Config.ReviewerModel.Name
		key := strings.TrimPrefix(id, e.ID+":")
		_, err = c.Provider.RecordEvidence(ctx, r.ID, guard(e, r, key), workcontrol.EvidenceSubmission{ID: id, PlanRevisionID: s.PlanID,
			Type: "semantic_review", Producer: s.Implementer, Verifier: s.Reviewer, SubjectDigest: candidate.Digest(), SourceRevision: s.Head,
			Command: command, Outcome: outcome, ProvenanceURI: c.artifactURL(digest), ProvenanceDigest: digest,
			ObservedAt: result.ObservedAt, ExpiresAt: result.ObservedAt.Add(24 * time.Hour), ArtifactURI: c.artifactURL(digest),
			ArtifactDigest: digest, PolicyVersion: c.Config.PolicyVersion})
		if err != nil {
			return err
		}
		e, err = c.Journal.GetExecution(ctx, e.ID)
		if err != nil {
			return err
		}
		_, _, _, err = c.Journal.AddEvidence(ctx, e.ID, e.Version, execution.EvidenceInput{Key: id, Kind: "semantic_review", SourceRevision: s.Head,
			Environment: "read-only kagent reviewer " + s.Reviewer, Command: command, Summary: outcome, URI: c.artifactURL(digest), ObservedAt: result.ObservedAt})
		if err != nil {
			return err
		}
		s.ReviewEvidenceID = id
		return c.save(ctx, e, s)
	}
	if outcome == "failed" {
		return c.failedAttempt(ctx, e, s, "review", digest, "Independent review rejected the candidate patch")
	}
	s.Step = "verify"
	return c.save(ctx, e, s)
}

func (s state) completionEvidence() []string {
	ids := append([]string{}, s.EvidenceIDs...)
	if s.ReviewEvidenceID != "" {
		ids = append(ids, s.ReviewEvidenceID)
	}
	if s.LandingEvidenceID != "" {
		ids = append(ids, s.LandingEvidenceID)
	}
	return ids
}
