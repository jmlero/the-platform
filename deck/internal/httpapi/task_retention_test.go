package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestOperatorCanDeleteDraftAndRestoreArchivedTask(t *testing.T) {
	handler := newTestHandler(t)
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		if response.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
		}
		return response
	}
	var task domain.Task
	response := request("POST", "/api/v1/tasks", `{"title":"Cleanup draft"}`, http.StatusCreated)
	if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/operator/tasks/" + task.ID
	request("DELETE", path, `{}`, http.StatusUnprocessableEntity)
	request("PUT", path+"/archive", `{"version":1}`, http.StatusUnprocessableEntity)
	response = request("PUT", path+"/archive", `{"version":1,"archived":true}`, http.StatusOK)
	if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if !task.Archived {
		t.Fatal("task not archived")
	}
	request("DELETE", path, fmt.Sprintf(`{"version":%d}`, task.Version), http.StatusConflict)
	response = request("GET", "/api/v1/bootstrap", "", http.StatusOK)
	var snapshot bootstrapResponse
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range snapshot.Tasks {
		if item.ID == task.ID {
			found = item.Archived
		}
	}
	if !found {
		t.Fatal("archive unavailable to operator")
	}
	response = request("PUT", path+"/archive", fmt.Sprintf(`{"version":%d,"archived":false}`, task.Version), http.StatusOK)
	if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	request("DELETE", path, `{"version":1}`, http.StatusConflict)
	request("DELETE", path, fmt.Sprintf(`{"version":%d}`, task.Version), http.StatusNoContent)
	request("GET", path, "", http.StatusNotFound)
}
