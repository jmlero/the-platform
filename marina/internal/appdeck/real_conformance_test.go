package appdeck

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workcontrol/conformance"
)

func TestRealAppDeckConformance(t *testing.T) {
	serverURL := os.Getenv("APPDECK_CONFORMANCE_URL")
	fixtureDirectory := os.Getenv("APPDECK_FIXTURES_DIR")
	if serverURL == "" && fixtureDirectory == "" {
		t.Skip("set APPDECK_CONFORMANCE_URL and APPDECK_FIXTURES_DIR to run the real-provider suite")
	}
	if serverURL == "" || fixtureDirectory == "" {
		t.Fatal("APPDECK_CONFORMANCE_URL and APPDECK_FIXTURES_DIR must be set together")
	}

	fixtures, err := conformance.LoadFixtures(fixtureDirectory)
	if err != nil {
		t.Fatalf("load AppDeck fixtures: %v", err)
	}
	client, err := New(serverURL, &http.Client{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("create AppDeck client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := waitForProvider(ctx, client); err != nil {
		t.Fatalf("wait for real AppDeck process: %v", err)
	}

	seeded, err := seedConformanceTask(ctx, client, fixtures.Task)
	if err != nil {
		t.Fatalf("seed conformance task: %v", err)
	}
	result, err := conformance.Verify(ctx, client, fixtures, seeded.ID)
	if err != nil {
		t.Fatalf("real AppDeck conformance: %v", err)
	}
	if result.Run.WorkItemID != seeded.ID || result.Run.AgentID != fixtures.Agent.ID {
		t.Fatalf("conformance result lost identity: %#v", result)
	}

	_, err = client.GetWork(ctx, "task_missing_conformance_fixture")
	assertFixtureError(t, err, fixtures.Errors["not_found"])
	_, err = client.ClaimWork(ctx, seeded.ID, workcontrol.Claim{
		AgentID: fixtures.Agent.ID,
		Lease:   time.Minute,
	})
	assertFixtureError(t, err, fixtures.Errors["conflict"])
	var ignored []taskDTO
	err = client.do(ctx, http.MethodGet, "/api/v1/tasks?status=not-a-status", nil, &ignored)
	assertFixtureError(t, err, fixtures.Errors["invalid_input"])
	err = client.do(ctx, http.MethodPost, "/api/v1/runs/"+result.Run.ID+"/start", nil, nil)
	assertFixtureError(t, err, fixtures.Errors["invalid_transition"])
}

func waitForProvider(ctx context.Context, client *Client) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		if _, err := client.GetProviderInfo(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%v: %w", lastErr, ctx.Err())
		case <-ticker.C:
		}
	}
}

func seedConformanceTask(
	ctx context.Context,
	client *Client,
	fixture conformance.TaskFixture,
) (workcontrol.WorkItem, error) {
	request := struct {
		ProjectID          string `json:"project_id"`
		Title              string `json:"title"`
		Description        string `json:"description"`
		AcceptanceCriteria string `json:"acceptance_criteria"`
		Status             string `json:"status"`
		Priority           string `json:"priority"`
		BudgetMinutes      int    `json:"budget_minutes"`
	}{
		ProjectID:          fixture.ProjectID,
		Title:              fixture.Title,
		Description:        fixture.Description,
		AcceptanceCriteria: fixture.AcceptanceCriteria,
		Status:             fixture.Status,
		Priority:           fixture.Priority,
		BudgetMinutes:      fixture.BudgetMinutes,
	}
	var response taskDTO
	if err := client.do(ctx, http.MethodPost, "/api/v1/tasks", request, &response); err != nil {
		return workcontrol.WorkItem{}, err
	}
	return response.toDomain(), nil
}

func assertFixtureError(t *testing.T, err error, fixture conformance.ErrorFixture) {
	t.Helper()
	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("error = %v, want AppDeck APIError", err)
	}
	if apiError.StatusCode != fixture.HTTPStatus ||
		apiError.Code != fixture.Code ||
		apiError.Retryable != fixture.Retryable ||
		apiError.RequestID == "" {
		t.Fatalf("API error = %#v, want fixture semantics %#v", apiError, fixture)
	}
}
