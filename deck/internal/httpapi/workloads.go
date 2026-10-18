package httpapi

import (
	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
	"net/http"
)

func (s *Server) workloadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/operator/messages/{message}/prd/tasks/approve", s.requireOperator(s.approvePRDTasks))
	mux.HandleFunc("GET /api/v1/operator/tasks/{task}/prd", s.requireOperator(s.getPRDProgress))
	mux.HandleFunc("POST /api/v1/operator/tasks/{task}/prd/accept", s.requireOperator(s.acceptPRD))
	mux.HandleFunc("POST /api/v1/operator/messages/{message}/prd/approve", s.requireOperator(s.approvePRD))
	mux.HandleFunc("GET /api/v1/operator/projects/{project}/workloads", s.requireOperator(s.getWorkloads))
	mux.HandleFunc("POST /api/v1/operator/projects/{project}/workloads/invoke", s.requireOperator(s.invokeWorkload))
}
func (s *Server) getWorkloads(w http.ResponseWriter, r *http.Request) {
	result, err := s.store.GetWorkloads(r.Context(), r.PathValue("project"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
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

func (s *Server) approvePRD(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" {
		s.writeAPIError(w, http.StatusForbidden, errorCodeForbidden, "an operator session is required", false)
		return
	}
	var input domain.PRDApproval
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
	task, err := s.store.ApprovePRD(r.Context(), r.PathValue("message"), mutation, input)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}
