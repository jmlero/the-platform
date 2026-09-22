package workerapi

import (
	"encoding/json"
	"errors"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/contextpack"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// The first reusable HTTP tool reads only selected, persisted inputs. The
// credential is the existing attempt reference; no upstream secret or URL is
// accepted from the harness. Audit metadata never contains source contents.
func (server *Server) contextTool(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		http.Error(w, "worker credential required", http.StatusUnauthorized)
		return
	}
	attempt, record, err := server.authorize(r.Context(), r.PathValue("id"), token)
	audit := func(allowed bool, reason, argumentDigest string) bool {
		if record.ID == "" {
			return true
		}
		payload := map[string]any{"type": "tool.decision", "message": "Context tool: " + reason, "tool": "read_context_source", "attempt_id": attempt.ID, "allowed": allowed, "reason": reason, "argument_digest": argumentDigest, "context_digest": attempt.Specification.ContextDigest}
		_, _, saveErr := server.journal.EnqueueOutbox(r.Context(), record.ID, execution.OutboxInput{Kind: "event", IdempotencyKey: "tool:" + attempt.ID + ":" + reason + ":" + argumentDigest, Payload: payload})
		if saveErr != nil {
			http.Error(w, "tool decision could not be recorded", 503)
			return false
		}
		return true
	}
	deny := func(status int, reason, digest string) {
		if audit(false, reason, digest) {
			http.Error(w, "context tool denied: "+reason, status)
		}
	}
	if err != nil {
		status := 403
		if errors.Is(err, errProviderUnavailable) || errors.Is(err, workcontrol.ErrContextUnavailable) {
			status = 503
		}
		deny(status, "authority_unavailable", "")
		return
	}
	if !slices.Contains(attempt.Specification.Tools, "read_context_source") {
		deny(403, "not_allowlisted", "")
		return
	}
	var args struct {
		URI string `json:"uri"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	d.DisallowUnknownFields()
	if d.Decode(&args) != nil || d.Decode(new(any)) != io.EOF || args.URI == "" || len(args.URI) > 1024 {
		deny(400, "invalid_arguments", "")
		return
	}
	digest := artifacts.Digest([]byte(args.URI))
	manifest, err := server.journal.GetContextPackage(r.Context(), attempt.Specification.ContextDigest)
	if err != nil || manifest.ExecutionID != record.ID || manifest.ValidateAt(time.Now()) != nil {
		deny(403, "context_unavailable", digest)
		return
	}
	for _, input := range manifest.Inputs {
		if input.URI != args.URI {
			continue
		}
		if input.Kind != contextpack.KindExternal && input.Kind != contextpack.KindRepositoryInstructions && input.Kind != contextpack.KindArchitectureDecision {
			break
		}
		if !audit(true, "allowed", digest) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(input)
		return
	}
	deny(403, "source_not_selected", digest)
}
