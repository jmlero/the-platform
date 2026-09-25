package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

var (
	errInvalidConversationProject = fmt.Errorf("project is required: %w", domain.ErrInvalidInput)
	errInvalidConversationCursor  = fmt.Errorf("nonnegative after_id is required: %w", domain.ErrInvalidInput)
)

func (s *Server) listConversationInvocations(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectID == "" {
		s.writeError(w, errInvalidConversationProject)
		return
	}
	if !s.requireProject(w, r, projectID) {
		return
	}
	afterID, err := strconv.ParseInt(r.URL.Query().Get("after_id"), 10, 64)
	if err != nil || afterID < 0 {
		s.writeError(w, errInvalidConversationCursor)
		return
	}
	items, err := s.store.ListConversationInvocations(r.Context(), projectID, afterID, 100)
	if err != nil {
		s.writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
