package httpapi

import (
	"github.com/jmlero/the-platform/deck/internal/domain"
	"net/http"
	"strconv"
)

func (s *Server) invokeConversation(w http.ResponseWriter, r *http.Request) {
	channel, err := s.store.GetChannel(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	var input domain.ConversationInvocationInput
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	input.ChannelID = channel.ID
	operator, _ := operatorSessionFromRequest(r)
	input.AuthorID = "operator:" + operator.OperatorID
	result, err := s.store.InvokeConversation(r.Context(), input)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) createThread(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RootMessageID string `json:"root_message_id"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	thread, err := s.store.CreateThread(r.Context(), r.PathValue("id"), input.RootMessageID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, thread)
}

func (s *Server) listThreads(w http.ResponseWriter, r *http.Request) {
	threads, err := s.store.ListChannelThreads(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": threads})
}

func (s *Server) getThread(w http.ResponseWriter, r *http.Request) {
	thread, err := s.store.GetThread(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

func (s *Server) getTaskThread(w http.ResponseWriter, r *http.Request) {
	thread, err := s.store.GetTaskThread(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

func (s *Server) linkThreadTask(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TaskID string `json:"task_id"`
	}
	if err := readJSON(r, &input); err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.store.LinkThreadTask(r.Context(), r.PathValue("id"), input.TaskID); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listThreadMessages(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := s.store.PageThreadMessages(r.Context(), r.PathValue("id"), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
