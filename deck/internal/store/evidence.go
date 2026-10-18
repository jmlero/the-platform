package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

const evidenceColumns = `
	id, task_id, run_id, plan_revision_id, type, producer, verifier,
	subject_digest, source_revision, command, outcome, provenance_uri,
	provenance_digest, observed_at, expires_at, artifact_uri, artifact_digest,
	policy_version, admission_status, created_at
`

func scanEvidence(scanner interface{ Scan(...any) error }) (domain.EvidenceRecord, error) {
	var evidence domain.EvidenceRecord
	var expiresAt sql.NullInt64
	if err := scanner.Scan(
		&evidence.ID, &evidence.TaskID, &evidence.RunID, &evidence.PlanRevisionID,
		&evidence.Type, &evidence.Producer, &evidence.Verifier, &evidence.SubjectDigest,
		&evidence.SourceRevision, &evidence.Command, &evidence.Outcome,
		&evidence.ProvenanceURI, &evidence.ProvenanceDigest, &evidence.ObservedAt,
		&expiresAt, &evidence.ArtifactURI, &evidence.ArtifactDigest,
		&evidence.PolicyVersion, &evidence.AdmissionStatus, &evidence.CreatedAt,
	); err != nil {
		return domain.EvidenceRecord{}, err
	}
	if expiresAt.Valid {
		value := expiresAt.Int64
		evidence.ExpiresAt = &value
	}
	return evidence, nil
}

// RecordEvidence stores one structured evidence item. An exact replay of the
// same ID returns the stored item; a changed one conflicts.
func (s *Store) RecordEvidence(ctx context.Context, input domain.EvidenceInput) (domain.EvidenceRecord, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.RunID = strings.TrimSpace(input.RunID)
	input.PlanRevisionID = strings.TrimSpace(input.PlanRevisionID)
	input.Type = strings.TrimSpace(input.Type)
	input.Producer = strings.TrimSpace(input.Producer)
	input.Verifier = strings.TrimSpace(input.Verifier)
	input.SubjectDigest = strings.TrimSpace(input.SubjectDigest)
	input.SourceRevision = strings.TrimSpace(input.SourceRevision)
	input.Command = strings.TrimSpace(input.Command)
	input.Outcome = strings.ToLower(strings.TrimSpace(input.Outcome))
	input.ProvenanceURI = strings.TrimSpace(input.ProvenanceURI)
	input.ProvenanceDigest = strings.TrimSpace(input.ProvenanceDigest)
	input.ArtifactURI = strings.TrimSpace(input.ArtifactURI)
	input.ArtifactDigest = strings.TrimSpace(input.ArtifactDigest)
	input.PolicyVersion = strings.TrimSpace(input.PolicyVersion)
	// CI and landing observations may have no retained artifact file.
	if input.RunID == "" || input.PlanRevisionID == "" || input.Type == "" ||
		input.Producer == "" || input.Verifier == "" || input.SubjectDigest == "" ||
		input.SourceRevision == "" || input.Command == "" || input.ProvenanceURI == "" ||
		input.ProvenanceDigest == "" || (input.ArtifactURI == "") != (input.ArtifactDigest == "") ||
		input.ObservedAt <= 0 {
		return domain.EvidenceRecord{}, fmt.Errorf("structured evidence fields are required: %w", domain.ErrInvalidInput)
	}
	if input.Outcome != "passed" && input.Outcome != "failed" {
		return domain.EvidenceRecord{}, fmt.Errorf("evidence outcome must be passed or failed: %w", domain.ErrInvalidInput)
	}

	now := nowMillis()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.EvidenceRecord{}, fmt.Errorf("begin evidence record: %w", err)
	}
	defer tx.Rollback()
	run, err := getRunTx(ctx, tx, input.RunID)
	if err != nil {
		return domain.EvidenceRecord{}, err
	}
	revision, err := scanPlanRevision(tx.QueryRowContext(ctx,
		"SELECT "+planRevisionColumns+" FROM plan_revisions WHERE id = ? AND run_id = ?",
		input.PlanRevisionID, input.RunID,
	))
	if err != nil {
		return domain.EvidenceRecord{}, normalizeStoreError(err)
	}
	status := "current"
	if run.ActivePlanRevisionID == nil || *run.ActivePlanRevisionID != input.PlanRevisionID {
		status = "superseded"
	} else if (input.ExpiresAt != nil && *input.ExpiresAt <= now) ||
		(revision.SourceRevision != "" && input.SourceRevision != revision.SourceRevision) ||
		(revision.PolicyVersion != "" && input.PolicyVersion != revision.PolicyVersion) {
		status = "stale"
	}
	id := input.ID
	if id == "" {
		id = newID("evd")
	}
	evidence := domain.EvidenceRecord{
		ID: id, TaskID: run.TaskID, RunID: input.RunID, PlanRevisionID: input.PlanRevisionID,
		Type: input.Type, Producer: input.Producer, Verifier: input.Verifier,
		SubjectDigest: input.SubjectDigest, SourceRevision: input.SourceRevision,
		Command: input.Command, Outcome: input.Outcome, ProvenanceURI: input.ProvenanceURI,
		ProvenanceDigest: input.ProvenanceDigest, ObservedAt: input.ObservedAt,
		ExpiresAt: input.ExpiresAt, ArtifactURI: input.ArtifactURI,
		ArtifactDigest: input.ArtifactDigest, PolicyVersion: input.PolicyVersion,
		AdmissionStatus: status, CreatedAt: now,
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO evidence_records (
			id, task_id, run_id, plan_revision_id, type, producer, verifier,
			subject_digest, source_revision, command, outcome, provenance_uri,
			provenance_digest, observed_at, expires_at, artifact_uri, artifact_digest,
			policy_version, admission_status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, evidence.ID, evidence.TaskID, evidence.RunID, evidence.PlanRevisionID,
		evidence.Type, evidence.Producer, evidence.Verifier, evidence.SubjectDigest,
		evidence.SourceRevision, evidence.Command, evidence.Outcome, evidence.ProvenanceURI,
		evidence.ProvenanceDigest, evidence.ObservedAt, nullableInt64(evidence.ExpiresAt),
		evidence.ArtifactURI, evidence.ArtifactDigest, evidence.PolicyVersion,
		evidence.AdmissionStatus, evidence.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			existing, loadErr := scanEvidence(tx.QueryRowContext(ctx,
				"SELECT "+evidenceColumns+" FROM evidence_records WHERE id = ?", evidence.ID,
			))
			if loadErr == nil && existing.RunID == evidence.RunID && existing.ProvenanceDigest == evidence.ProvenanceDigest && existing.ArtifactDigest == evidence.ArtifactDigest {
				return existing, nil
			}
			return domain.EvidenceRecord{}, fmt.Errorf("evidence ID was reused with different content: %w", domain.ErrConflict)
		}
		return domain.EvidenceRecord{}, fmt.Errorf("insert evidence: %w", err)
	}
	event, err := insertEventTx(ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"evidence.recorded", "Structured evidence recorded: "+evidence.Type,
		map[string]any{"evidence_id": evidence.ID, "outcome": evidence.Outcome, "admission_status": evidence.AdmissionStatus, "plan_revision_id": evidence.PlanRevisionID},
	)
	if err != nil {
		return domain.EvidenceRecord{}, fmt.Errorf("create evidence event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.EvidenceRecord{}, fmt.Errorf("commit evidence: %w", err)
	}
	s.publish(event)
	return evidence, nil
}

func (s *Store) listEvidenceForTask(ctx context.Context, taskID string) ([]domain.EvidenceRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+evidenceColumns+" FROM evidence_records WHERE task_id = ? ORDER BY created_at DESC, id", taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("list task evidence: %w", err)
	}
	defer rows.Close()
	items := make([]domain.EvidenceRecord, 0)
	for rows.Next() {
		item, scanErr := scanEvidence(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan task evidence: %w", scanErr)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CompleteRunWithEvidence(
	ctx context.Context,
	mutation Mutation,
	runID string,
	summary string,
	evidenceIDs []string,
) error {
	return s.completeRun(ctx, mutation, runID, summary, evidenceIDs, nil)
}

func (s *Store) CompleteLandedRun(ctx context.Context, mutation Mutation, runID, summary string, evidenceIDs []string, landing domain.LandingObservation) error {
	return s.completeRun(ctx, mutation, runID, summary, evidenceIDs, &landing)
}

func (s *Store) completeRun(ctx context.Context, mutation Mutation, runID, summary string, evidenceIDs []string, landing *domain.LandingObservation) error {
	summary = strings.TrimSpace(summary)
	if summary == "" || len(evidenceIDs) == 0 {
		return fmt.Errorf("summary and structured evidence are required: %w", domain.ErrInvalidInput)
	}
	projectID, err := s.ProjectIDForRun(ctx, runID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin structured completion: %w", err)
	}
	defer tx.Rollback()
	attempt, replayResult, replayed, err := beginIdempotentMutation(
		ctx, tx, mutation, projectID, operationCompleteRun, runID,
		struct {
			RunID       string                     `json:"run_id"`
			Summary     string                     `json:"summary"`
			EvidenceIDs []string                   `json:"evidence_ids"`
			Guard       *RunGuard                  `json:"guard,omitempty"`
			Landing     *domain.LandingObservation `json:"landing,omitempty"`
		}{runID, summary, evidenceIDs, mutation.Guard, landing},
	)
	if err != nil {
		return err
	}
	if replayed {
		var original struct{}
		return decodeIdempotencyResult(replayResult, &original)
	}
	run, err := getRunTx(ctx, tx, runID)
	if err != nil {
		return err
	}
	if err := validateRunGuard(run, mutation.Guard); err != nil {
		return err
	}
	if run.Status != domain.RunVerifying || run.ActivePlanRevisionID == nil {
		return fmt.Errorf("run is not verifying an active plan: %w", domain.ErrInvalidTransition)
	}
	revision, err := scanPlanRevision(tx.QueryRowContext(ctx,
		"SELECT "+planRevisionColumns+" FROM plan_revisions WHERE id = ? AND run_id = ?",
		*run.ActivePlanRevisionID, run.ID,
	))
	if err != nil {
		return normalizeStoreError(err)
	}

	ids := append([]string(nil), evidenceIDs...)
	sort.Strings(ids)
	items := make([]domain.EvidenceRecord, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	now := nowMillis()
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		item, loadErr := scanEvidence(tx.QueryRowContext(ctx,
			"SELECT "+evidenceColumns+" FROM evidence_records WHERE id = ? AND run_id = ?", id, runID,
		))
		if loadErr != nil {
			return normalizeStoreError(loadErr)
		}
		if item.ExpiresAt != nil && *item.ExpiresAt <= now {
			item.AdmissionStatus = "stale"
			if _, updateErr := tx.ExecContext(ctx,
				"UPDATE evidence_records SET admission_status = 'stale' WHERE id = ?", item.ID,
			); updateErr != nil {
				return fmt.Errorf("mark expired evidence stale: %w", updateErr)
			}
		}
		if item.PlanRevisionID != revision.ID || item.AdmissionStatus != "current" ||
			item.Outcome != "passed" || item.Producer == item.Verifier ||
			(revision.SourceRevision != "" && item.SourceRevision != revision.SourceRevision) ||
			(revision.PolicyVersion != "" && item.PolicyVersion != revision.PolicyVersion) {
			return fmt.Errorf("evidence %s is not an admissible current independent verification: %w", item.ID, domain.ErrConflict)
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return fmt.Errorf("no admissible evidence was supplied: %w", domain.ErrInvalidInput)
	}
	var delivered *domain.TaskLanding
	if landing != nil {
		task, err := getTaskTx(ctx, tx, run.TaskID)
		if err != nil {
			return err
		}
		criteria := sha256.Sum256([]byte(task.AcceptanceCriteria))
		if hex.EncodeToString(criteria[:]) != run.AcceptanceCriteriaDigest {
			return fmt.Errorf("acceptance criteria changed after authorization; a new verified run is required: %w", domain.ErrConflict)
		}
		if err = validateLanding(task, *landing, items, now); err != nil {
			return err
		}
		delivered = &domain.TaskLanding{LandingObservation: *landing, RunID: run.ID, EvidenceIDs: ids}
	}
	report := renderVerificationReport(summary, revision, items)
	digestValue := sha256.Sum256([]byte(report))
	reportDigest := "sha256:" + hex.EncodeToString(digestValue[:])
	artifact := domain.Artifact{
		ID: newID("art"), TaskID: run.TaskID, RunID: run.ID,
		Name: "verification-report.md", Kind: "verification", Content: report,
		Digest: reportDigest, SizeBytes: int64(len(report)), CreatedAt: now,
	}
	artifact.URI = "deck://artifacts/" + artifact.ID
	result, err := tx.ExecContext(ctx, `
		UPDATE runs
		SET status = 'completed', result_summary = ?, lease_expires_at = NULL,
			finished_at = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND status = 'verifying' AND version = ?
	`, summary, now, now, runID, run.Version)
	if err != nil {
		return fmt.Errorf("complete structured run: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("run cannot complete: %w", domain.ErrInvalidTransition)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = 'done', assigned_agent_id = NULL,
			version = version + 1, updated_at = ?, landing_json = ? WHERE id = ?
	`, now, marshalJSON(delivered), run.TaskID); err != nil {
		return fmt.Errorf("complete task: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE agents SET status = 'idle', last_seen_at = ? WHERE id = ?", now, run.AgentID,
	); err != nil {
		return fmt.Errorf("idle agent: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifacts (id, task_id, run_id, name, kind, uri, content, digest, size_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, artifact.ID, artifact.TaskID, artifact.RunID, artifact.Name, artifact.Kind,
		artifact.URI, artifact.Content, artifact.Digest, artifact.SizeBytes, artifact.CreatedAt); err != nil {
		return fmt.Errorf("create verification report: %w", err)
	}
	event, err := insertEventTx(ctx, tx, &run.TaskID, &run.ID, &run.AgentID,
		"run.completed", "Run completed with structured verification evidence",
		map[string]any{"artifact_id": artifact.ID, "artifact_digest": artifact.Digest, "evidence_ids": ids, "summary": summary},
	)
	if err != nil {
		return fmt.Errorf("create completion event: %w", err)
	}
	if err := finishIdempotentMutation(ctx, tx, attempt, struct{}{}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit structured completion: %w", err)
	}
	s.publish(event)
	return nil
}

func renderVerificationReport(summary string, revision domain.PlanRevision, items []domain.EvidenceRecord) string {
	var report strings.Builder
	report.WriteString("# Verification report\n\n")
	report.WriteString(summary)
	report.WriteString("\n\n")
	fmt.Fprintf(&report, "- Plan revision: %d (`%s`)\n", revision.Revision, revision.ID)
	fmt.Fprintf(&report, "- Source revision: `%s`\n", revision.SourceRevision)
	fmt.Fprintf(&report, "- Policy version: `%s`\n", revision.PolicyVersion)
	report.WriteString("\n| Evidence | Outcome | Producer | Verifier | Command | Artifact digest |\n")
	report.WriteString("|---|---|---|---|---|---|\n")
	for _, item := range items {
		fmt.Fprintf(&report, "| `%s` | %s | %s | %s | `%s` | `%s` |\n",
			item.ID, item.Outcome, markdownCell(item.Producer), markdownCell(item.Verifier),
			markdownCell(item.Command), item.ArtifactDigest)
	}
	return report.String()
}

func markdownCell(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}
