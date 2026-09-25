package httpapi

import (
	"fmt"
	"net/http"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func (s *Server) deleteDraft(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version int `json:"version"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	if input.Version < 1 {
		s.writeError(w, fmt.Errorf("version is required: %w", domain.ErrInvalidInput))
		return
	}
	if err := s.store.DeleteDraft(r.Context(), r.PathValue("id"), input.Version); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setTaskArchived(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version  int   `json:"version"`
		Archived *bool `json:"archived"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	if input.Version < 1 || input.Archived == nil {
		s.writeError(w, fmt.Errorf("version and archived are required: %w", domain.ErrInvalidInput))
		return
	}
	task, err := s.store.SetTaskArchived(r.Context(), r.PathValue("id"), input.Version, *input.Archived)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}
