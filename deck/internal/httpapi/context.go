package httpapi

import (
	"net/http"
	"strconv"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func (s *Server) contextRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/operator/projects/{project}/repository", s.requireOperator(s.getRepository))
	mux.HandleFunc("GET /api/v1/operator/projects/{project}/context/sources", s.requireOperator(s.listContextSources))
	mux.HandleFunc("PUT /api/v1/operator/projects/{project}/context/sources", s.requireOperator(s.putContextSource))
	mux.HandleFunc("POST /api/v1/operator/projects/{project}/context/sources/{source}/{revision}/revoke", s.requireOperator(s.revokeContextSource))
	mux.HandleFunc("GET /api/v1/operator/tasks/{id}/context", s.requireOperator(s.taskContext))
	mux.HandleFunc("GET /api/v1/operator/runs/{id}/context", s.requireOperator(s.runContext))
}

func (s *Server) listContextSources(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListContextSources(r.Context(), r.PathValue("project"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) putContextSource(w http.ResponseWriter, r *http.Request) {
	var input domain.ContextSourceInput
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	item, err := s.store.PutContextSource(r.Context(), r.PathValue("project"), input)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) revokeContextSource(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.ParseInt(r.PathValue("revision"), 10, 64)
	if err != nil || revision < 1 {
		s.writeError(w, domain.ErrInvalidInput)
		return
	}
	if err = s.store.RevokeContextSource(r.Context(), r.PathValue("project"), r.PathValue("source"), revision); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (s *Server) taskContext(w http.ResponseWriter, r *http.Request) {
	result, err := s.store.ResolveTaskContext(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) runContext(w http.ResponseWriter, r *http.Request) {
	result, err := s.store.ResolveRunContext(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) getRepository(w http.ResponseWriter, r *http.Request) {
	result, err := s.store.GetRepository(r.Context(), r.PathValue("project"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
