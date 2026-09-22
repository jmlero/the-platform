package workerapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	rt "github.com/jmlero/the-platform/marina/internal/runtime"
)

// AdmitEngine implements kagent.Admission without adding another authority
// store. Recovery compares the exact persisted specification and asks Deck and
// the configured context validator again. Read-only inspection needs no grant.
func (server *Server) AdmitEngine(ctx context.Context, spec rt.Specification) error {
	if spec.Engine == nil || len(server.config.SigningKey) < 32 {
		return rt.ErrInvalidInput
	}
	if _, err := spec.EngineGatewayReference(); err != nil {
		return err
	}
	attempt, err := server.journal.GetRuntimeAttempt(ctx, spec.AttemptID)
	if err != nil {
		return err
	}
	digest, err := spec.Digest()
	if err != nil || digest != attempt.SpecificationHash {
		return rt.ErrConflict
	}
	if engine, err := server.journal.GetEngineAttempt(ctx, attempt.ID); err == nil {
		if engine.CancelRequested || engine.DeleteRequested || engine.Deleted || engine.State.Terminal() || engine.Phase == "revision_mismatch" {
			return rt.ErrConflict
		}
	} else if !errors.Is(err, execution.ErrNotFound) {
		return err
	}
	_, record, err := server.authorizeState(ctx, attempt)
	if err != nil {
		return err
	}
	// Model calls still make their own atomic reservation. This also stops new
	// dispatch when existing unknown calls have consumed the remaining allocation.
	totals, err := server.journal.ExecutionModelTotals(ctx, record.ID)
	if err != nil {
		return err
	}
	if record.MaxTokens > 0 && record.UsedTokens+totals.ReservedTokens >= record.MaxTokens ||
		record.MaxCostMicros > 0 && record.UsedCostMicros+totals.ReservedCostMicros >= record.MaxCostMicros {
		return execution.ErrBudgetExhausted
	}
	return nil
}

// EngineCredential is installed only in the attempt's Substrate-injected
// Kubernetes Secret. Never place its returned value in task/config/evidence
// bytes. Secret values do not participate in kagent's prepared revision, so the
// credential is issued after the exact revision and runtime attempt are bound.
func (server *Server) EngineCredential(ctx context.Context, spec rt.Specification) (string, error) {
	if err := server.AdmitEngine(ctx, spec); err != nil {
		return "", err
	}
	return spec.AttemptID + "." + kubernetes.Token(server.config.SigningKey, spec), nil
}

// Native Codex cannot interpolate an attempt ID into a shared ModelConfig URL.
// Its injected credential carries that selector plus the existing specification
// HMAC. Each route then uses the same admission, reservations and tool/source
// controls as a worker. Arbitrary routes are deliberately not forwarded.
func (server *Server) engineRoute(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			http.Error(w, "engine gateway requires TLS", 403)
			return
		}
		bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		id, token, found := strings.Cut(bearer, ".")
		if !ok || !found || len(id) > 128 || !strings.HasPrefix(id, "rtm_") || len(token) != 64 || strings.ContainsAny(id, "/\\ \t\r\n") {
			http.Error(w, "attempt credential required", 401)
			return
		}
		attempt, err := server.journal.GetRuntimeAttempt(r.Context(), id)
		if err != nil || attempt.Specification.Engine == nil {
			http.Error(w, "engine attempt unavailable", 403)
			return
		}
		request := r.Clone(r.Context())
		request.SetPathValue("id", id)
		request.Header.Set("Authorization", "Bearer "+token)
		next(w, request)
	}
}
