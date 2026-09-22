package httpapi

import (
	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
	"net/http"
)

func (s *Server) workloadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/operator/projects/{project}/workloads", s.requireOperator(s.getWorkloads))
	mux.HandleFunc("POST /api/v1/operator/projects/{project}/workloads/invoke", s.requireOperator(s.invokeWorkload))
	mux.HandleFunc("PUT /api/v1/projects/{project}/workloads", s.requireService(capabilityWorkloadPublish, s.publishWorkloads))
}
func (s *Server) getWorkloads(w http.ResponseWriter, r *http.Request) {
	result, err := s.store.GetWorkloads(r.Context(), r.PathValue("project"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (s *Server) publishWorkloads(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w, r, r.PathValue("project")) {
		return
	}
	var input domain.WorkloadCatalog
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	owner := "local-prototype"
	if id, ok := serviceIdentityFromRequest(r); ok {
		owner = "service:" + id.ID
	}
	if err := s.store.PublishWorkloads(r.Context(), r.PathValue("project"), owner, input); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"recorded": true})
}
func (s *Server) invokeWorkload(w http.ResponseWriter, r *http.Request) {
	var input domain.WorkloadInvocation
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	if len(r.Header.Values("Idempotency-Key")) != 1 {
		s.writeError(w, domain.ErrInvalidInput)
		return
	}
	mutation, err := store.NewMutation("operator:"+operatorIDFromRequest(r), r.Header.Get("Idempotency-Key"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	task, err := s.store.InvokeWorkload(r.Context(), r.PathValue("project"), mutation, input)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, 201, task)
}
