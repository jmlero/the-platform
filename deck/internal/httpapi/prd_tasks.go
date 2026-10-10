package httpapi

import (
	"github.com/jmlero/the-platform/deck/internal/domain"
	"github.com/jmlero/the-platform/deck/internal/store"
	"net/http"
)

func (s *Server) prdOperatorMutation(w http.ResponseWriter, r *http.Request) (store.Mutation, bool) {
	if r.Header.Get("Authorization") != "" {
		s.writeServiceAuthError(w, ErrServiceForbidden)
		return store.Mutation{}, false
	}
	if len(r.Header.Values("Idempotency-Key")) != 1 {
		s.writeError(w, domain.ErrInvalidInput)
		return store.Mutation{}, false
	}
	m, err := store.NewMutation("operator:"+operatorIDFromRequest(r), r.Header.Get("Idempotency-Key"))
	if err != nil {
		s.writeError(w, err)
		return m, false
	}
	return m, true
}
func (s *Server) approvePRDTasks(w http.ResponseWriter, r *http.Request) {
	m, ok := s.prdOperatorMutation(w, r)
	if !ok {
		return
	}
	var in domain.PRDApproval
	if err := readJSON(r, &in); err != nil {
		s.writeError(w, err)
		return
	}
	tasks, err := s.store.ApprovePRDTasks(r.Context(), r.PathValue("message"), m, in)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, 201, tasks)
}
func (s *Server) getPRDProgress(w http.ResponseWriter, r *http.Request) {
	progress, err := s.store.GetPRDProgress(r.Context(), r.PathValue("task"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, 200, progress)
}
func (s *Server) acceptPRD(w http.ResponseWriter, r *http.Request) {
	m, ok := s.prdOperatorMutation(w, r)
	if !ok {
		return
	}
	var in struct {
		ResultsDigest string `json:"results_digest"`
	}
	if err := readJSON(r, &in); err != nil {
		s.writeError(w, err)
		return
	}
	progress, err := s.store.AcceptPRD(r.Context(), r.PathValue("task"), m, in.ResultsDigest)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, 200, progress)
}
