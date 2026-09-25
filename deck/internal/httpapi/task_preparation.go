package httpapi

import (
	"github.com/jmlero/the-platform/deck/internal/domain"
	"net/http"
)

func (s *Server) acceptTask(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version         int  `json:"version"`
		SourceAvailable bool `json:"source_available"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	actor, ok := operatorSessionFromRequest(r)
	if !ok {
		s.writeError(w, domain.ErrInvalidInput)
		return
	}
	task, err := s.store.AcceptTask(r.Context(), r.PathValue("id"), input.Version, actor.OperatorID, input.SourceAvailable)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) setTaskDependencies(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version   int       `json:"version"`
		DependsOn *[]string `json:"depends_on"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	if input.DependsOn == nil {
		s.writeError(w, domain.ErrInvalidInput)
		return
	}
	task, err := s.store.SetTaskDependencies(r.Context(), r.PathValue("id"), input.Version, *input.DependsOn)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}
