package httpapi

import (
	"net/http"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func (s *Server) publishConversationOutcome(w http.ResponseWriter, r *http.Request) {
	var input domain.ConversationOutcomeInput
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	invocation, err := s.store.GetChannelMessage(r.Context(), input.InvocationMessageID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	if !s.requireProject(w, r, invocation.ProjectID) {
		return
	}
	ready := input.Task != nil && input.Task.Status == domain.TaskReady
	if ready != (r.URL.Path == "/api/v1/conversation/delegations") {
		s.writeServiceAuthError(w, ErrServiceForbidden)
		return
	}
	outcome, err := s.store.PublishConversationOutcome(r.Context(), input)
	if err != nil {
		s.writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, outcome)
}
