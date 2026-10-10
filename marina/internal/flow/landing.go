package flow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func (c *Controller) advanceLanding(ctx context.Context, e execution.Execution, r workcontrol.Run, s state) error {
	source, err := c.Source(e)
	if err != nil {
		return err
	}
	head, err := source.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: c.Config.Git.Repository, Ref: s.Branch})
	if err != nil {
		return err
	}
	if head.Commit != s.Head {
		return c.block(ctx, e, r, "Task branch changed after independent verification; earlier evidence cannot complete this run")
	}
	if s.Step == "ci" || s.Step == "land" {
		base, err := source.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: c.Config.Git.Repository, Ref: c.Config.Git.BaseBranch})
		if err != nil {
			return err
		}
		landed := base.Commit == s.Head
		if base.Commit != s.baseRevision() && base.Commit != s.Head {
			// A successful landing response may be lost before another writer
			// advances main. Observe the exact task commit before admitting
			// another bounded rebase of work that is already on main.
			if ancestry, ok := source.(sourcecontrol.AncestryPort); ok {
				landed, err = ancestry.ContainsCommit(ctx, c.Config.Git.Repository, s.Head, base.Commit)
				if err != nil {
					return err
				}
			}
			if !landed {
				return c.beginRebase(ctx, e, r, s, base.Commit)
			}
		}
		checks, err := source.ObserveChecks(ctx, sourcecontrol.ObservationRequest{Repository: c.Config.Git.Repository, HeadRevision: s.Head})
		if err != nil {
			return err
		}
		for _, required := range c.Config.RequiredCI {
			found := false
			for _, check := range checks {
				if check.ID != required || check.HeadRevision != s.Head {
					continue
				}
				found = true
				if check.State == "failure" || check.State == "failed" || check.State == "cancelled" || check.State == "timed_out" || check.State == "action_required" {
					return c.failedCI(ctx, e, s, checks, "Required CI failed on the task branch: "+required)
				}
				if check.State != "success" {
					return nil
				}
			}
			if !found {
				return nil
			}
		}
		if s.Step == "ci" {
			s.Step = "land"
			return c.save(ctx, e, s)
		}
		if !landed {
			landing, ok := source.(sourcecontrol.LandingPort)
			if !ok {
				return errors.New("configured source does not support guarded fast-forward")
			}
			revision, err := landing.FastForward(ctx, sourcecontrol.FastForwardRequest{Repository: c.Config.Git.Repository, BaseRef: c.Config.Git.BaseBranch, ExpectedBase: s.baseRevision(), HeadRef: s.Branch, ExpectedHead: s.Head, AuthorityFence: e.FenceToken})
			if err != nil {
				return err
			}
			if revision.Commit != s.Head || revision.Ref != c.Config.Git.BaseBranch {
				return errors.New("landing response did not confirm the exact commit on the base branch")
			}
		}
		s.Landing = &workcontrol.LandingObservation{Repository: c.Config.Git.Repository, BaseBranch: c.Config.Git.BaseBranch, Commit: s.Head, ObservedAt: time.Now().UnixMilli()}
		proof, err := json.Marshal(struct {
			Landing workcontrol.LandingObservation
			Checks  []sourcecontrol.CheckObservation
		}{*s.Landing, checks})
		if err != nil {
			return err
		}
		s.LandingDigest, err = c.Artifacts.Put(proof)
		if err != nil {
			return err
		}
		if r.Status != workcontrol.RunVerifying {
			if err := c.Provider.BeginVerification(ctx, r.ID, guard(e, r, "verify")); err != nil {
				return err
			}
		}
		if e.State != execution.StateVerifying {
			if err := c.transition(ctx, e, execution.StateVerifying, "Required CI passed; exact commit landed on the base branch"); err != nil {
				return err
			}
		}
		s.Step = "evidence"
		return c.save(ctx, e, s)
	}
	if s.Step == "evidence" {
		if err := c.confirmLanded(ctx, source, s); err != nil {
			if errors.Is(err, errLandingUnavailable) {
				return c.block(ctx, e, r, err.Error())
			}
			return err
		}
		if s.LandingEvidenceID == "" {
			return c.landingEvidence(ctx, e, r, s)
		}
		return c.evidence(ctx, e, r, s)
	}
	if s.Step == "complete" {
		detail, err := c.Provider.GetWork(ctx, e.ExternalWorkID)
		if err != nil {
			return err
		}
		criteria := sha256.Sum256([]byte(detail.Work.AcceptanceCriteria))
		if hex.EncodeToString(criteria[:]) != r.AcceptanceCriteriaDigest {
			return c.block(ctx, e, r, "Acceptance criteria changed after authorization; authorize a new task for the edited scope")
		}
		if err := c.confirmLanded(ctx, source, s); err != nil {
			if errors.Is(err, errLandingUnavailable) {
				return c.block(ctx, e, r, err.Error())
			}
			return err
		}
		provider, ok := c.Provider.(workcontrol.LandedCompletion)
		if !ok || s.Landing == nil || s.LandingEvidenceID == "" {
			return c.block(ctx, e, r, "Provider cannot complete an evidenced landing automatically")
		}
		if err := provider.CompleteLandedRun(ctx, r.ID, guard(e, r, "complete"), fmt.Sprintf("Verified commit landed on %s: %s", c.Config.Git.BaseBranch, s.Head), s.completionEvidence(), *s.Landing); err != nil {
			return err
		}
		return c.transition(ctx, e, execution.StateCompleted, "Verified commit landed on the base branch")
	}
	return fmt.Errorf("unknown landing step %q", s.Step)
}

var errLandingUnavailable = errors.New("landed commit unavailable on the authorized base branch")

func (c *Controller) confirmLanded(ctx context.Context, source sourcecontrol.Port, s state) error {
	current, err := source.CurrentHead(ctx, sourcecontrol.HeadRequest{Repository: c.Config.Git.Repository, Ref: c.Config.Git.BaseBranch})
	if err != nil {
		return err
	}
	if current.Commit == s.Head {
		return nil
	}
	ancestry, ok := source.(sourcecontrol.AncestryPort)
	if !ok {
		return fmt.Errorf("cannot prove the landed commit remains on the base branch: %w", errLandingUnavailable)
	}
	contained, err := ancestry.ContainsCommit(ctx, c.Config.Git.Repository, s.Head, current.Commit)
	if err != nil {
		return err
	}
	if !contained {
		return errLandingUnavailable
	}
	return nil
}

func (c *Controller) landingEvidence(ctx context.Context, e execution.Execution, r workcontrol.Run, s state) error {
	if s.Landing == nil || s.LandingDigest == "" {
		return c.block(ctx, e, r, "The exact landing and CI observation is missing")
	}
	if _, err := c.Artifacts.Get(s.LandingDigest); err != nil {
		return err
	}
	id := operationKey(e, s, "landing-evidence")
	observed := time.UnixMilli(s.Landing.ObservedAt).UTC()
	command := "required CI and guarded fast-forward to " + s.Landing.BaseBranch
	_, err := c.Provider.RecordEvidence(ctx, r.ID, guard(e, r, strings.TrimPrefix(id, e.ID+":")), workcontrol.EvidenceSubmission{ID: id, PlanRevisionID: s.PlanID, Type: "landing", Producer: s.Implementer, Verifier: "git-provider", SubjectDigest: s.Head, SourceRevision: s.Head, Command: command, Outcome: "passed", ProvenanceURI: c.artifactURL(s.LandingDigest), ProvenanceDigest: s.LandingDigest, ObservedAt: observed, ArtifactURI: c.artifactURL(s.LandingDigest), ArtifactDigest: s.LandingDigest, PolicyVersion: c.Config.PolicyVersion})
	if err != nil {
		return err
	}
	e, err = c.Journal.GetExecution(ctx, e.ID)
	if err != nil {
		return err
	}
	_, _, _, err = c.Journal.AddEvidence(ctx, e.ID, e.Version, execution.EvidenceInput{Key: id, Kind: "landing", SourceRevision: s.Head, Environment: "Git provider " + s.Landing.Repository, Command: command, Summary: "passed", URI: c.artifactURL(s.LandingDigest), ObservedAt: observed})
	if err != nil {
		return err
	}
	s.LandingEvidenceID = id
	return c.save(ctx, e, s)
}
