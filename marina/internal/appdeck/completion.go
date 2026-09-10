package appdeck

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

func (client *Client) mutateRun(ctx context.Context, id, path string, guard workcontrol.Mutation, body map[string]any, result any) error {
	if err := validatePathSegment("run ID", id); err != nil {
		return err
	}
	if strings.TrimSpace(guard.IdempotencyKey) == "" || guard.AgentID == "" || guard.ExpectedVersion < 1 || guard.FenceToken < 1 {
		return errors.New("durable run mutation requires complete authority and an idempotency key")
	}
	if body == nil {
		body = make(map[string]any)
	}
	body["agent_id"] = guard.AgentID
	body["expected_version"] = guard.ExpectedVersion
	body["fence_token"] = guard.FenceToken
	return client.doIdempotent(ctx, http.MethodPost, "/api/v1/runs/"+id+"/"+path, guard.IdempotencyKey, body, result)
}

func (client *Client) StartRun(ctx context.Context, id string, guard workcontrol.Mutation) error {
	return client.mutateRun(ctx, id, "start", guard, nil, nil)
}

func (client *Client) BeginVerification(ctx context.Context, id string, guard workcontrol.Mutation) error {
	return client.mutateRun(ctx, id, "verify", guard, nil, nil)
}

func (client *Client) RecordEvidence(ctx context.Context, id string, guard workcontrol.Mutation, evidence workcontrol.EvidenceSubmission) (string, error) {
	body := map[string]any{"id": evidence.ID, "run_id": id, "plan_revision_id": evidence.PlanRevisionID,
		"type": evidence.Type, "producer": evidence.Producer, "verifier": evidence.Verifier,
		"subject_digest": evidence.SubjectDigest, "source_revision": evidence.SourceRevision,
		"command": evidence.Command, "outcome": evidence.Outcome, "provenance_uri": evidence.ProvenanceURI,
		"provenance_digest": evidence.ProvenanceDigest, "observed_at": evidence.ObservedAt.UnixMilli(),
		"artifact_uri": evidence.ArtifactURI, "artifact_digest": evidence.ArtifactDigest, "policy_version": evidence.PolicyVersion}
	if !evidence.ExpiresAt.IsZero() {
		body["expires_at"] = evidence.ExpiresAt.UnixMilli()
	}
	var result struct {
		ID string `json:"id"`
	}
	err := client.mutateRun(ctx, id, "evidence", guard, body, &result)
	if err == nil && result.ID != evidence.ID {
		return "", errors.New("provider returned a different evidence identity")
	}
	return result.ID, err
}

func (client *Client) CompleteRun(ctx context.Context, id string, guard workcontrol.Mutation, summary string, evidenceIDs []string) error {
	if len(evidenceIDs) == 0 {
		return errors.New("completion requires structured evidence IDs")
	}
	body := map[string]any{"summary": summary, "evidence_ids": evidenceIDs}
	return client.mutateRun(ctx, id, "complete", guard, body, nil)
}
