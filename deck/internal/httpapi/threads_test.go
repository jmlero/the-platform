package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestThreadIsSharedByIndependentOperatorSessions(t *testing.T) {
	s, handler, _ := newOperatorAuthTestHandler(t)
	ctx := context.Background()
	channel, err := s.CreateChannel(ctx, "prj_default", "threads")
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Existing draft", Status: domain.TaskBacklog})
	if err != nil {
		t.Fatal(err)
	}
	login := func() []*http.Cookie {
		response := requestJSON(t, handler, http.MethodPost, "/api/v1/operator/session", "", map[string]any{"token": operatorAuthToken})
		if response.Code != http.StatusCreated {
			t.Fatalf("login: %d %s", response.Code, response.Body.String())
		}
		return response.Result().Cookies()
	}
	first, second := login(), login()
	send := func(cookies []*http.Cookie, method, path string, body any, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(string(data)))
		req.Header.Set("Content-Type", "application/json")
		for _, cookie := range cookies {
			req.AddCookie(cookie)
			if csrf && cookie.Name == operatorCSRFCookieName {
				req.Header.Set(operatorCSRFHeader, cookie.Value)
			}
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	rootResponse := send(first, "POST", "/api/v1/operator/channels/"+channel.ID+"/messages", map[string]any{"body": "Investigate login"}, true)
	if rootResponse.Code != 201 {
		t.Fatalf("root: %d %s", rootResponse.Code, rootResponse.Body.String())
	}
	var root domain.ChannelMessage
	if err := json.Unmarshal(rootResponse.Body.Bytes(), &root); err != nil {
		t.Fatal(err)
	}
	createPath := "/api/v1/operator/channels/" + channel.ID + "/threads"
	created := send(first, "POST", createPath, map[string]any{"root_message_id": root.ID}, true)
	if created.Code != 201 {
		t.Fatalf("create thread: %d %s", created.Code, created.Body.String())
	}
	var thread domain.ConversationThread
	if err := json.Unmarshal(created.Body.Bytes(), &thread); err != nil {
		t.Fatal(err)
	}
	replay := send(second, "POST", createPath, map[string]any{"root_message_id": root.ID}, true)
	if replay.Code != 201 || replay.Body.String() != created.Body.String() {
		t.Fatalf("second session replay: %d %s", replay.Code, replay.Body.String())
	}
	base := "/api/v1/operator/threads/" + thread.ID
	denied := send(first, "POST", base+"/tasks", map[string]any{"task_id": task.ID}, false)
	assertAPIError(t, denied, http.StatusForbidden, errorCodeForbidden)
	linked := send(first, "POST", base+"/tasks", map[string]any{"task_id": task.ID}, true)
	if linked.Code != 204 {
		t.Fatalf("link: %d %s", linked.Code, linked.Body.String())
	}
	read := send(second, "GET", "/api/v1/operator/tasks/"+task.ID+"/thread", nil, false)
	if read.Code != 200 {
		t.Fatalf("task thread: %d %s", read.Code, read.Body.String())
	}
	var restored domain.ConversationThread
	if err := json.Unmarshal(read.Body.Bytes(), &restored); err != nil {
		t.Fatal(err)
	}
	if restored.ID != thread.ID || len(restored.TaskIDs) != 1 || restored.TaskIDs[0] != task.ID {
		t.Fatalf("task origin: %#v", restored)
	}
	reply := send(second, "POST", "/api/v1/operator/channels/"+channel.ID+"/messages", map[string]any{"body": "Second device reply", "thread_id": thread.ID}, true)
	if reply.Code != 201 {
		t.Fatalf("reply: %d %s", reply.Code, reply.Body.String())
	}
	history := send(first, "GET", base+"/messages", nil, false)
	var messages struct {
		Items []domain.ChannelMessage `json:"items"`
	}
	if err := json.Unmarshal(history.Body.Bytes(), &messages); err != nil {
		t.Fatal(err)
	}
	if history.Code != 200 || len(messages.Items) != 2 || messages.Items[1].ThreadID != thread.ID {
		t.Fatalf("shared history: %d %s", history.Code, history.Body.String())
	}
	listed := send(second, "GET", createPath, nil, false)
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), thread.ID) {
		t.Fatalf("list: %d %s", listed.Code, listed.Body.String())
	}
	detail := send(second, "GET", base, nil, false)
	if detail.Code != 200 || detail.Body.String() != read.Body.String() {
		t.Fatalf("detail: %d %s", detail.Code, detail.Body.String())
	}
	unchanged, err := s.GetTask(ctx, task.ID)
	if err != nil || unchanged.Status != domain.TaskBacklog || unchanged.Version != task.Version {
		t.Fatalf("link changed execution intent: %#v %v", unchanged, err)
	}
}

func TestOperatorConversationInvocationRequiresCSRFAndReplaysAcrossSessions(t *testing.T) {
	s, handler, _ := newOperatorAuthTestHandler(t)
	ctx := context.Background()
	channel, err := s.CreateChannel(ctx, "prj_default", "invocations")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterAgent(ctx, domain.Agent{ID: "agt_conversation", InstallationID: "conversation-installation", Name: "Conversation", Capabilities: []string{"conversation.invoke.prj_default"}}); err != nil {
		t.Fatal(err)
	}
	login := func() []*http.Cookie {
		res := requestJSON(t, handler, http.MethodPost, "/api/v1/operator/session", "", map[string]any{"token": operatorAuthToken})
		if res.Code != http.StatusCreated {
			t.Fatalf("login: %d", res.Code)
		}
		return res.Result().Cookies()
	}
	send := func(cookies []*http.Cookie, csrf bool, body any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/v1/operator/channels/"+channel.ID+"/invocations", strings.NewReader(string(data)))
		for _, cookie := range cookies {
			req.AddCookie(cookie)
			if csrf && cookie.Name == operatorCSRFCookieName {
				req.Header.Set(operatorCSRFHeader, cookie.Value)
			}
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	first, second := login(), login()
	input := map[string]any{"agent_id": "agt_conversation", "body": "Discuss login", "request_id": "http-request-1"}
	assertAPIError(t, send(first, false, input), http.StatusForbidden, errorCodeForbidden)
	created := send(first, true, input)
	if created.Code != http.StatusCreated {
		t.Fatalf("invoke: %d %s", created.Code, created.Body.String())
	}
	replay := send(second, true, input)
	if replay.Code != http.StatusCreated || replay.Body.String() != created.Body.String() {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
	}
	input["body"] = "Changed"
	assertAPIError(t, send(second, true, input), http.StatusConflict, errorCodeConflict)
}
