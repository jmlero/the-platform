package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestProjectChannelCreationIsIsolatedAndReplayable(t *testing.T) {
	s, h := newTestStoreAndHandler(t)
	input := map[string]any{"repository": "example/project", "base_branch": "main"}
	first := requestJSON(t, h, "POST", "/api/v1/operator/projects", "", input)
	if first.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	var created struct {
		Project  domain.Project   `json:"project"`
		Channels []domain.Channel `json:"channels"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	again := requestJSON(t, h, "POST", "/api/v1/operator/projects", "", input)
	if again.Code != http.StatusCreated || first.Body.String() != again.Body.String() {
		t.Fatalf("replay: %d %s", again.Code, again.Body.String())
	}
	if created.Project.ID == "prj_default" || len(created.Channels) != 1 || created.Channels[0].ProjectID != created.Project.ID {
		t.Fatalf("project/channel identity: %+v", created)
	}
	ctx := context.Background()
	msg, err := s.PostChannelMessage(ctx, domain.ChannelMessageInput{ChannelID: created.Channels[0].ID, AuthorType: "human", AuthorID: "human", Body: "Capture an idea"})
	if err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, h, "POST", "/api/v1/operator/messages/"+msg.ID+"/promote", "", map[string]any{"title": "Refine later", "status": "backlog"})
	if response.Code != http.StatusCreated {
		t.Fatalf("draft requires premature details: %d %s", response.Code, response.Body.String())
	}
	var task domain.Task
	json.Unmarshal(response.Body.Bytes(), &task)
	if task.ProjectID != created.Project.ID || task.Status != domain.TaskBacklog {
		t.Fatalf("wrong project draft: %+v", task)
	}
}

func TestDispatchWaitsForDependencies(t *testing.T) {
	s, h := newTestStoreAndHandler(t)
	ctx := context.Background()
	prerequisite, _ := s.CreateTask(ctx, domain.CreateTaskInput{Title: "First"})
	dependent, _ := s.CreateTask(ctx, domain.CreateTaskInput{Title: "Second"})
	response := requestJSON(t, h, "PUT", "/api/v1/operator/tasks/"+dependent.ID+"/dependencies", "", map[string]any{"version": dependent.Version, "depends_on": []string{prerequisite.ID}})
	if response.Code != 200 {
		t.Fatalf("dependencies: %s", response.Body.String())
	}
	s.MoveTask(ctx, dependent.ID, domain.TaskReady)
	ready, err := s.MarinaReadyTasks(ctx, []string{"prj_default"})
	if err != nil || len(ready) != 0 {
		t.Fatalf("ineligible task offered to Marina: %+v %v", ready, err)
	}
}
