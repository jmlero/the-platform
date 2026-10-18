package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func (s *Server) listConversationQuestions(w http.ResponseWriter, r *http.Request) {
	thread, err := s.store.GetThread(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	items, err := s.store.ListConversationQuestions(r.Context(), thread.ID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// answerConversationQuestion forwards the answer to Marina's Decision before
// recording the canonical human reply in the thread.
func (s *Server) answerConversationQuestion(w http.ResponseWriter, r *http.Request) {
	q, err := s.store.GetConversationQuestion(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
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
	answer := strings.TrimSpace(input.Answer)
	if q.Status == "pending" && answer != "" && strings.TrimSpace(input.RequestID) != "" {
		turn, err := s.store.GetMarinaTurn(r.Context(), q.InvocationMessageID)
		if err == nil {
			if !s.requireMarina(w) {
				return
			}
			if _, err := s.marina.AnswerDecision(r.Context(), turn.Namespace, q.QuestionID, answer, "deck-"+q.ID); err != nil {
				s.writeMarinaError(w, err)
				return
			}
		} else if !errors.Is(err, domain.ErrNotFound) {
			s.writeError(w, err)
			return
		}
	}
	operator, _ := operatorSessionFromRequest(r)
	q, err = s.store.AnswerConversationQuestion(r.Context(), q.ID, input.Answer, "operator:"+strings.TrimSpace(operator.OperatorID), input.RequestID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}
