package deck

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestWorkInspectionPreservesAuthorizedBudget(t *testing.T) {
	client, err := New("https://deck.example", &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{"task": map[string]any{
			"id": "task", "project_id": "project", "version": 3, "budget_minutes": 25,
			"budget": map[string]any{"time_minutes": 25, "deadline_at": int64(1800000000000),
				"max_attempts": 2, "max_tokens": 50000, "max_cost_minor": 700,
				"currency": "EUR", "max_concurrency": 1, "authority": []string{"propose"}},
		}}), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := client.GetWork(context.Background(), "task")
	if err != nil {
		t.Fatal(err)
	}
	budget := detail.Work.Budget
	if budget.TimeMinutes != 25 || budget.MaxAttempts != 2 || budget.MaxTokens != 50000 ||
		budget.MaxCostMinor != 700 || budget.Currency != "EUR" || budget.MaxConcurrency != 1 ||
		budget.DeadlineAt == nil || !budget.DeadlineAt.Equal(time.UnixMilli(1800000000000)) ||
		len(budget.Authority) != 1 || budget.Authority[0] != "propose" {
		t.Fatalf("task authorization was lost: %#v", budget)
	}
}
