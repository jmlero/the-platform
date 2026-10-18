package flow

import (
	"strings"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

func installation() Config {
	c := Config{ProjectID: "factory.acceptance", MaxTokens: 400000, BudgetMinutes: 120, Delivery: "fast_forward", RequiredCI: []string{"test"},
		AllowedPaths: []string{"src", "tests"}, Checks: []workresult.Check{{ID: "unit", Directory: ".", Command: []string{"node", "--test"}}},
		ReviewerModel: &workerapi.Model{Name: "reviewer"}}
	return c
}

func TestFactoryChoicesShapeTheExecutionConfig(t *testing.T) {
	c, err := installation().WithChoices(&workcontrol.FactoryChoices{Checks: []workcontrol.FactoryCheck{{ID: "lint", Directory: "web", Command: []string{"npm", "run", "lint"}}},
		AllowedPaths: []string{"web"}, RequireApproval: true, Review: false, Delivery: "pull_request", RepairFailures: 1, MaxTokens: 1000, BudgetMinutes: 10})
	if err != nil {
		t.Fatal(err)
	}
	if c.Delivery != "pull_request" || !c.RequireApproval || !c.SkipReview || c.failureLimit() != 1 || c.MaxTokens != 1000 || c.BudgetMinutes != 10 ||
		len(c.Checks) != 1 || c.Checks[0].ID != "lint" || c.AllowedPaths[0] != "web" {
		t.Fatalf("config = %+v", c)
	}
	if c.Digest() == installation().Digest() {
		t.Fatal("choices did not change the execution profile digest")
	}
	same, _ := installation().WithChoices(&workcontrol.FactoryChoices{Checks: []workcontrol.FactoryCheck{{ID: "lint", Directory: "web", Command: []string{"npm", "run", "lint"}}},
		AllowedPaths: []string{"web"}, RequireApproval: true, Review: false, Delivery: "pull_request", RepairFailures: 1, MaxTokens: 1000, BudgetMinutes: 10})
	if same.Digest() != c.Digest() {
		t.Fatal("the same choices produced a different digest")
	}
	if none, err := installation().WithChoices(nil); err != nil || none.Digest() != installation().Digest() {
		t.Fatalf("no choices must keep the installation profile: %v", err)
	}
}

func TestFactoryChoicesCannotExceedTheInstallation(t *testing.T) {
	base := workcontrol.FactoryChoices{Checks: []workcontrol.FactoryCheck{{ID: "unit", Directory: ".", Command: []string{"node", "--test"}}}, AllowedPaths: []string{"src"}, Review: true, Delivery: "fast_forward", RequiredCI: []string{"test"}, RepairFailures: 3}
	for name, change := range map[string]func(*workcontrol.FactoryChoices){
		"more tokens":         func(c *workcontrol.FactoryChoices) { c.MaxTokens = 500000 },
		"more minutes":        func(c *workcontrol.FactoryChoices) { c.BudgetMinutes = 500 },
		"land without CI":     func(c *workcontrol.FactoryChoices) { c.RequiredCI = nil },
		"unknown delivery":    func(c *workcontrol.FactoryChoices) { c.Delivery = "deploy" },
		"no checks":           func(c *workcontrol.FactoryChoices) { c.Checks = nil },
		"no paths":            func(c *workcontrol.FactoryChoices) { c.AllowedPaths = nil },
		"unbounded repairs":   func(c *workcontrol.FactoryChoices) { c.RepairFailures = 9 },
		"escaping path":       func(c *workcontrol.FactoryChoices) { c.AllowedPaths = []string{"../etc"} },
		"empty check command": func(c *workcontrol.FactoryChoices) { c.Checks[0].Command = nil },
	} {
		choices := base
		choices.Checks = append([]workcontrol.FactoryCheck(nil), base.Checks...)
		change(&choices)
		if _, err := installation().WithChoices(&choices); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	reviewless := installation()
	reviewless.ReviewerModel = nil
	if _, err := reviewless.WithChoices(&base); err == nil || !strings.Contains(err.Error(), "review") {
		t.Fatalf("review without a reviewer model: %v", err)
	}
}
