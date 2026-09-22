package flow

import (
	"encoding/json"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

func TestAnalysisEvidenceBindsContextForVersionTwo(t *testing.T) {
	store, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := Controller{Artifacts: store}
	fixture := analysis.DiagnosticFixture()
	report := analysis.Report{Summary: "Two blocked runs", Findings: fixture.Expected}
	s := state{Analysis: &analysis.Profile{HarnessVersion: analysis.HarnessWithContext, Fixture: fixture}, ProfileDigest: "profile", Base: fixture.InputDigest(), ContextDigest: "frozen-context"}
	output, _ := json.Marshal(worker.Result{Analysis: &report, ProfileDigest: s.ProfileDigest, InputDigest: s.Base})
	s.ImplementationDigest, err = store.Put(output)
	if err != nil {
		t.Fatal(err)
	}
	proof := analysis.Validation{Version: 1, Validator: "marina:diagnostic-validator/v1", ProfileDigest: s.ProfileDigest, InputDigest: s.Base, OutputDigest: s.ImplementationDigest, Report: report, Sources: fixture.Sources}
	for _, digest := range []string{"", "different-context", s.ContextDigest} {
		proof.ContextDigest = digest
		raw, _ := json.Marshal(proof)
		s.VerificationDigest, err = store.Put(raw)
		if err != nil {
			t.Fatal(err)
		}
		err = c.checkAnalysisEvidence(s)
		if (err == nil) != (digest == s.ContextDigest) {
			t.Fatalf("context %q: %v", digest, err)
		}
	}
	// Completed v1 records predate the optional context binding.
	s.Analysis.HarnessVersion = analysis.Harness
	proof.ContextDigest = ""
	raw, _ := json.Marshal(proof)
	s.VerificationDigest, err = store.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.checkAnalysisEvidence(s); err != nil {
		t.Fatal(err)
	}
}
