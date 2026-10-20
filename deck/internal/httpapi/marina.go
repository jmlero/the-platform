package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/marina"
)

// Marina forwarding. Deck records an operator action only after Marina has
// accepted it, so the ledger never claims an answer or cancellation Marina
// did not receive. Replays use stable idempotency keys.

// executorStatus marks the provider unavailable when Deck has no executor.
func (s *Server) executorStatus(status domain.ProviderStatus) domain.ProviderStatus {
	if s.marina == nil {
		status.Connectivity, status.SyncState = "unavailable", "unavailable"
	}
	return status
}

// writeMarinaError keeps Marina's own Status message, which the API defines
// as safe, and logs transport detail.
func (s *Server) writeMarinaError(w http.ResponseWriter, err error) {
	var status *marina.APIError
	if !errors.As(err, &status) {
		s.logger.Error("Marina request failed", "error", err)
		s.writeAPIError(w, http.StatusBadGateway, errorCodeServiceUnavailable, "Marina is unavailable; try again", true)
		return
	}
	s.logger.Warn("Marina refused a request", "code", status.Code, "reason", status.Reason, "message", status.Message)
	message := strings.TrimSpace(status.Message)
	switch {
	case status.Code == http.StatusConflict:
		s.writeAPIError(w, http.StatusConflict, errorCodeConflict, "Marina did not accept this: "+message, false)
	case status.Code == http.StatusBadRequest || status.Code == http.StatusUnprocessableEntity:
		s.writeAPIError(w, http.StatusUnprocessableEntity, errorCodeInvalidInput, "Marina did not accept this: "+message, false)
	case status.Code == http.StatusNotFound:
		s.writeAPIError(w, http.StatusConflict, errorCodeConflict, "Marina no longer has this work", false)
	default:
		s.writeAPIError(w, http.StatusBadGateway, errorCodeServiceUnavailable, "Marina refused Deck's request", true)
	}
}

func (s *Server) requireMarina(w http.ResponseWriter) bool {
	if s.marina == nil {
		s.writeAPIError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "Marina is not configured", false)
		return false
	}
	return true
}

// createOperatorCommand forwards a cancel to Marina. Marina offers no pause,
// resume, replan or retry, so Deck rejects them instead of recording intent
// nothing will act on.
func (s *Server) createOperatorCommand(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	link, err := s.store.GetMarinaRun(r.Context(), r.PathValue("id"))
	if errors.Is(err, domain.ErrNotFound) {
		if _, runErr := s.store.GetRun(r.Context(), r.PathValue("id")); runErr != nil {
			s.writeError(w, runErr)
			return
		}
		s.writeAPIError(w, http.StatusConflict, errorCodeConflict, "this run is not executed by Marina and accepts no commands", false)
		return
	}
	if err != nil {
		s.writeError(w, err)
		return
	}
	if input.Type != domain.CommandCancel {
		s.writeAPIError(w, http.StatusConflict, errorCodeConflict, "Marina supports only cancel; "+strings.TrimSpace(input.Type)+" is not available", false)
		return
	}
	if strings.TrimSpace(input.Reason) == "" {
		s.writeError(w, domain.ErrInvalidInput)
		return
	}
	commands, err := s.store.ListCommands(r.Context(), link.Run.ID, 0, 200)
	if err != nil {
		s.writeError(w, err)
		return
	}
	for _, command := range commands {
		if command.Type == domain.CommandCancel && (command.Status == "pending" || command.Status == "acknowledged") {
			writeJSON(w, http.StatusOK, command)
			return
		}
	}
	if !s.requireMarina(w) {
		return
	}
	if !link.Created {
		// Marina has not confirmed the Run, so there is nothing to cancel there
		// yet; the engine withdraws it instead of creating it.
		command, err := s.store.CreateCommand(r.Context(), link.Run.ID, input.Type, input.Reason, "operator:"+operatorIDFromRequest(r))
		if err != nil {
			s.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, command)
		return
	}
	if err := s.marina.CancelRun(r.Context(), link.Namespace, link.Run.ID, "deck-cancel-"+link.Run.ID); err != nil {
		s.writeMarinaError(w, err)
		return
	}
	command, err := s.store.CreateCommand(r.Context(), link.Run.ID, input.Type, input.Reason, "operator:"+operatorIDFromRequest(r))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, command)
}

// answerDecision forwards a plan approval or run question to Marina first.
func (s *Server) answerDecision(w http.ResponseWriter, r *http.Request) {
	decision, err := s.store.GetDecision(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	var input struct {
		Answer string `json:"answer"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	if decision.Status == "pending" && decision.RunID != nil && strings.TrimSpace(input.Answer) != "" {
		link, err := s.store.GetMarinaRun(r.Context(), *decision.RunID)
		if err == nil {
			if !s.requireMarina(w) {
				return
			}
			name := decision.ID
			if decision.InteractionID != "" {
				name = decision.InteractionID
			}
			if _, err := s.marina.AnswerDecision(r.Context(), link.Namespace, name, strings.TrimSpace(input.Answer), "deck-"+decision.ID); err != nil {
				s.writeMarinaError(w, err)
				return
			}
		} else if !errors.Is(err, domain.ErrNotFound) {
			s.writeError(w, err)
			return
		}
	}
	decision, err = s.store.AnswerDecision(r.Context(), decision.ID, input.Answer)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

// downloadArtifact proxies one evidence file from Marina after checking that
// the task's ledger references it; Marina's client checks the digest.
func (s *Server) downloadArtifact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	detail, err := s.store.GetTaskDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	digest := r.PathValue("digest")
	runID := ""
	for _, evidence := range detail.Evidence {
		if digest != "" && evidence.ArtifactDigest == digest {
			runID = evidence.RunID
			break
		}
	}
	link, err := s.store.GetMarinaRun(r.Context(), runID)
	if runID == "" || errors.Is(err, domain.ErrNotFound) {
		http.Error(w, "No downloadable artifact is registered for this task", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, err)
		return
	}
	if s.marina == nil {
		http.Error(w, "Marina is not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := s.marina.RunArtifact(r.Context(), link.Namespace, link.Run.ID, digest)
	if err != nil {
		s.logger.Warn("Marina artifact download failed", "run", link.Run.ID, "error", err)
		http.Error(w, "Marina could not provide a digest-checked artifact", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="evidence-`+strings.TrimPrefix(digest, "sha256:")[:12]+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}
