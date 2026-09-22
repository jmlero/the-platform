package analysis

import (
	"strings"
	"testing"
)

func testProfile() Profile {
	return Profile{Version: 1, Revision: 1, Image: "analysis@sha256:" + strings.Repeat("a", 64), HarnessVersion: "langchain-create-agent/v1", MaxTokens: 20000, BudgetMinutes: 5, Fixture: DiagnosticFixture()}
}

func TestAnalysisProfilePinsItsFixtureAndRejectsUnsupportedConfiguration(t *testing.T) {
	p := testProfile()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	digest := p.Digest()
	p.Fixture.Sources[0].Content += " changed"
	if p.Digest() == digest {
		t.Fatal("input edit did not change the profile digest")
	}
	for _, mutate := range []func(*Profile){
		func(p *Profile) { p.Version = 2 }, func(p *Profile) { p.Image = "analysis:latest" },
		func(p *Profile) { p.HarnessVersion = "unknown" }, func(p *Profile) { p.MaxTokens = 0 },
		func(p *Profile) { p.Fixture.Expected = nil }, func(p *Profile) { p.Fixture.Expected[0].Quote = "invented" },
	} {
		p = testProfile()
		mutate(&p)
		if p.Validate() == nil {
			t.Fatalf("unsupported profile accepted: %+v", p)
		}
	}
}

func TestAnalysisValidationRequiresExactFindingsAndSourceQuotes(t *testing.T) {
	fixture := DiagnosticFixture()
	report := Report{Summary: "Two blocked runs require operator attention", Findings: append([]Finding{}, fixture.Expected...)}
	if err := ValidateReport(fixture, report); err != nil {
		t.Fatal(err)
	}
	withContext := report
	withContext.Findings = append([]Finding{}, report.Findings...)
	withContext.Findings[0].Quote = "Model gateway request failed: upstream HTTP 503"
	if err := ValidateReport(fixture, withContext); err != nil {
		t.Fatal("verbatim causal quote with context rejected:", err)
	}
	for _, mutate := range []func(*Report){
		func(r *Report) { r.Findings = r.Findings[:1] },
		func(r *Report) { r.Findings = append(r.Findings, r.Findings[0]) },
		func(r *Report) { r.Findings[0].SourceID = "unknown" },
		func(r *Report) { r.Findings[0].Quote = "made up" },
		func(r *Report) { r.Findings[0].Code = "all_healthy" },
		func(r *Report) { r.Summary = "" },
	} {
		r := report
		r.Findings = append([]Finding{}, report.Findings...)
		mutate(&r)
		if ValidateReport(fixture, r) == nil {
			t.Fatalf("invalid findings accepted: %+v", r)
		}
	}
}

func TestLegacyHarnessCannotAdvertiseContextTools(t *testing.T) {
	p := testProfile()
	p.Tools = []string{"read_context_source"}
	if p.Validate() == nil {
		t.Fatal("legacy image contract advertised a tool it does not implement")
	}
	p.HarnessVersion = "langchain-create-agent/v2"
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}
