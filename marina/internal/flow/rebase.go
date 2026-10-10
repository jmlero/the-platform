package flow

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

type rebaseAttempt struct {
	worker.Rebase
	NewBase      string
	FailureCount int
}

func (s state) baseRevision() string {
	if len(s.Rebases) != 0 {
		return s.Rebases[len(s.Rebases)-1].NewBase
	}
	return s.Base
}

func (s state) rebasePending() bool {
	return len(s.Rebases) != 0 && s.Head == s.baseRevision() && len(s.Failures) == s.Rebases[len(s.Rebases)-1].FailureCount
}

func (c *Controller) beginRebase(ctx context.Context, e execution.Execution, r workcontrol.Run, s state, base string) error {
	detail, err := c.Provider.GetWork(ctx, e.ExternalWorkID)
	if err != nil {
		return err
	}
	source, err := c.Source(e)
	if err != nil {
		return err
	}
	if err := c.confirmDependencies(ctx, source, detail.Work, base); err != nil {
		if errors.Is(err, errLandingUnavailable) {
			return c.block(ctx, e, r, err.Error())
		}
		return err
	}
	if reason, err := c.repairBudget(ctx, e, s, 3); err != nil {
		return err
	} else if reason != "" {
		return c.block(ctx, e, r, "Cannot rebase within the original budget: "+reason)
	}
	s.Rebases = append(s.Rebases, rebaseAttempt{Rebase: worker.Rebase{BaseRevision: s.baseRevision(), HeadRevision: s.Head, Branch: s.Branch}, NewBase: base, FailureCount: len(s.Failures)})
	s.Branch = fmt.Sprintf("marina/%s/rebase-%d", e.ID, len(s.Rebases))
	s.Head = base
	s.Implementer, s.Verifier, s.ImplementationDigest, s.ImplementationBase, s.VerificationDigest = "", "", "", "", ""
	s.EvidenceIDs = nil
	s.Reviewer, s.ReviewDigest, s.ReviewEvidenceID, s.CandidateDigest = "", "", "", ""
	s.Landing, s.LandingDigest, s.LandingEvidenceID = nil, "", ""
	s.EngineStatus, s.QuestionID, s.QuestionDecisionID, s.QuestionRunVersion = "", "", "", 0
	s.Step = "implement"
	return c.save(ctx, e, s)
}
