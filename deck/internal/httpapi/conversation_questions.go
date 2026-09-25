package httpapi

import (
	"net/http"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func (s *Server) ensureConversationQuestion(w http.ResponseWriter, r *http.Request) {
	var input domain.ConversationQuestionInput
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	// The invocation supplies the exact project scope; do not accept a project
	// claimed by the caller's request body.
	message, err := s.store.GetChannelMessage(r.Context(), input.InvocationMessageID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	if !s.requireProject(w, r, message.ProjectID) {
		return
	}
	q, err := s.store.EnsureConversationQuestion(r.Context(), input)
	if err != nil {
		s.writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, q)
}

func (s *Server) listConversationQuestions(w http.ResponseWriter, r *http.Request) {
	thread, err := s.store.GetThread(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	if !s.requireProject(w, r, thread.ProjectID) {
		return
	}
	items, err := s.store.ListConversationQuestions(r.Context(), thread.ID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) answerConversationQuestion(w http.ResponseWriter, r *http.Request) {
	q, err := s.store.GetConversationQuestion(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	if !s.requireProject(w, r, q.ProjectID) {
		return
	}
	var input struct {
		Answer    string `json:"answer"`
		RequestID string `json:"request_id"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	operator, _ := operatorSessionFromRequest(r)
	q, err = s.store.AnswerConversationQuestion(r.Context(), q.ID, input.Answer, "operator:"+strings.TrimSpace(operator.OperatorID), input.RequestID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}
