package httpapi

import "net/http"

func (s *Server) projectChannels(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ProjectChannels(r.Context())
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *Server) createProjectChannel(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Repository string `json:"repository"`
		BaseBranch string `json:"base_branch"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	item, err := s.store.CreateProjectChannel(r.Context(), input.Repository, input.BaseBranch)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
