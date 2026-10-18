package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestContextAPIRegistersRevisionsAndResolvesAuthorizedSelection(t *testing.T) {
	s, handler := newTestStoreAndHandler(t)
	endpoint := "/api/v1/operator/projects/prj_default/context/sources"
	input := domain.ContextSourceInput{ID: "goals", Title: "Goals", Kind: "goal", Content: "Keep the project small", Trust: "guidance", State: "active"}
	response := requestJSON(t, handler, http.MethodPut, endpoint, "", input)
	if response.Code != 200 {
		t.Fatalf("register source: %d %s", response.Code, response.Body.String())
	}
	var source domain.ContextSource
	if err := json.Unmarshal(response.Body.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(context.Background(), domain.CreateTaskInput{Title: "Use project goals", Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/v1/operator/tasks/"+task.ID+"/context", "", nil)
	var resolved domain.ResolvedContext
	if err := json.Unmarshal(response.Body.Bytes(), &resolved); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || len(resolved.Sources) != 1 || resolved.Sources[0].Digest != source.Digest {
		t.Fatalf("resolve context: %d %s", response.Code, response.Body.String())
	}
}
