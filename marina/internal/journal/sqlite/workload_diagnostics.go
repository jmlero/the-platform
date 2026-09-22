package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
)

type ModelTotals struct {
	Tokens             int64 `json:"reported_tokens"`
	CostMicros         int64 `json:"reported_cost_micros"`
	PendingCalls       int64 `json:"pending_model_calls"`
	ReservedTokens     int64 `json:"reserved_tokens"`
	ReservedCostMicros int64 `json:"reserved_cost_micros"`
}

func (s *Store) ExecutionModelTotals(ctx context.Context, id string) (ModelTotals, error) {
	var t ModelTotals
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(used_tokens),0),COALESCE(SUM(used_cost_micros),0),COALESCE(SUM(CASE WHEN finished_at IS NULL THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN finished_at IS NULL THEN reserved_tokens ELSE 0 END),0),COALESCE(SUM(CASE WHEN finished_at IS NULL THEN reserved_cost_micros ELSE 0 END),0) FROM model_calls WHERE execution_id=?`, id).Scan(&t.Tokens, &t.CostMicros, &t.PendingCalls, &t.ReservedTokens, &t.ReservedCostMicros)
	return t, err
}

type diagnosticModelCall struct {
	ID                 string `json:"id"`
	ExecutionID        string `json:"execution_id"`
	AttemptID          string `json:"attempt_id"`
	ReservedTokens     int64  `json:"reserved_tokens"`
	ReservedCostMicros int64  `json:"reserved_cost_micros"`
	UsedTokens         *int64 `json:"used_tokens"`
	UsedCostMicros     *int64 `json:"used_cost_micros"`
	UsageKnown         bool   `json:"usage_known"`
}
type diagnosticToolDecision struct {
	ExecutionID    string `json:"execution_id"`
	AttemptID      string `json:"attempt_id"`
	Tool           string `json:"tool"`
	Allowed        bool   `json:"allowed"`
	Reason         string `json:"reason"`
	ArgumentDigest string `json:"argument_digest"`
	ContextDigest  string `json:"context_digest"`
}
type diagnosticArtifact struct {
	ID          string `json:"id"`
	ExecutionID string `json:"execution_id"`
	AttemptID   string `json:"attempt_id"`
	Digest      string `json:"digest"`
}

func (s *Store) exportDiagnosticModelCalls(ctx context.Context, e *diagnosticExport) error {
	e.ModelCalls = []diagnosticModelCall{}
	rows, err := s.db.QueryContext(ctx, `SELECT id,execution_id,attempt_id,reserved_tokens,reserved_cost_micros,used_tokens,used_cost_micros FROM model_calls ORDER BY execution_id,created_at,id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c diagnosticModelCall
		var tokens, cost sql.NullInt64
		if err = rows.Scan(&c.ID, &c.ExecutionID, &c.AttemptID, &c.ReservedTokens, &c.ReservedCostMicros, &tokens, &cost); err != nil {
			return err
		}
		if tokens.Valid && cost.Valid {
			c.UsedTokens = &tokens.Int64
			c.UsedCostMicros = &cost.Int64
			c.UsageKnown = true
		}
		e.ModelCalls = append(e.ModelCalls, c)
	}
	return rows.Err()
}
func (s *Store) exportDiagnosticToolDecisions(ctx context.Context, e *diagnosticExport) error {
	e.ToolDecisions = []diagnosticToolDecision{}
	rows, err := s.db.QueryContext(ctx, `SELECT execution_id,payload_json FROM provider_outbox WHERE json_extract(payload_json,'$.type')='tool.decision' ORDER BY execution_id,sequence`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c diagnosticToolDecision
		var raw string
		if err = rows.Scan(&c.ExecutionID, &raw); err != nil {
			return err
		}
		var p struct {
			AttemptID      string `json:"attempt_id"`
			Tool           string `json:"tool"`
			Allowed        bool   `json:"allowed"`
			Reason         string `json:"reason"`
			ArgumentDigest string `json:"argument_digest"`
			ContextDigest  string `json:"context_digest"`
		}
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return err
		}
		c.AttemptID = p.AttemptID
		c.Tool = p.Tool
		c.Allowed = p.Allowed
		c.Reason = p.Reason
		c.ArgumentDigest = p.ArgumentDigest
		c.ContextDigest = p.ContextDigest
		e.ToolDecisions = append(e.ToolDecisions, c)
	}
	return rows.Err()
}
func (s *Store) exportDiagnosticArtifacts(ctx context.Context, e *diagnosticExport) error {
	e.RuntimeArtifacts = []diagnosticArtifact{}
	rows, err := s.db.QueryContext(ctx, `SELECT id,execution_id,runtime_attempt_id,digest FROM runtime_artifacts ORDER BY execution_id,id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c diagnosticArtifact
		if err = rows.Scan(&c.ID, &c.ExecutionID, &c.AttemptID, &c.Digest); err != nil {
			return err
		}
		e.RuntimeArtifacts = append(e.RuntimeArtifacts, c)
	}
	return rows.Err()
}
