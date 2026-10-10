package workresult

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func reviewFixture() (Result, Expected) {
	patch := "sha256:" + strings.Repeat("a", 64)
	result := Result{Stage: "review", SourceRevision: strings.Repeat("b", 40), InputDigest: strings.Repeat("c", 64), ProfileDigest: strings.Repeat("d", 64), ObservedAt: time.Now().UTC(), Review: &Review{PatchDigest: patch}}
	for _, id := range ReviewCheckIDs {
		result.Review.Checks = append(result.Review.Checks, ReviewCheck{ID: id, Outcome: "passed", Reason: "Evidence in the candidate", Citations: []Citation{{Path: "tests/regression_test.go", Line: 8}}})
	}
	return result, Expected{Stage: result.Stage, SourceRevision: result.SourceRevision, InputDigest: result.InputDigest, ProfileDigest: result.ProfileDigest, PatchDigest: patch}
}

func TestReviewRequiresEveryBoundVerdictAndCannotReturnChanges(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Result)
	}{
		{"different patch", func(r *Result) { r.Review.PatchDigest = "sha256:" + strings.Repeat("e", 64) }},
		{"different base", func(r *Result) { r.SourceRevision = strings.Repeat("e", 40) }},
		{"different input", func(r *Result) { r.InputDigest = strings.Repeat("e", 64) }},
		{"different profile", func(r *Result) { r.ProfileDigest = strings.Repeat("e", 64) }},
		{"missing tests", func(r *Result) { r.Review.Checks = r.Review.Checks[:2] }},
		{"duplicate check", func(r *Result) { r.Review.Checks[2].ID = "scope" }},
		{"unsupported outcome", func(r *Result) { r.Review.Checks[2].Outcome = "approve" }},
		{"missing explanation", func(r *Result) { r.Review.Checks[2].Reason = " " }},
		{"missing citation", func(r *Result) { r.Review.Checks[2].Citations = nil }},
		{"unsafe citation", func(r *Result) { r.Review.Checks[2].Citations[0].Path = "../outside" }},
		{"source changes", func(r *Result) { r.Changes = []Change{{Path: "src/edit.go"}} }},
		{"authority proposal", func(r *Result) { r.Repair = &RepairPlan{CanRepair: true} }},
		{"self reported usage", func(r *Result) { r.Tokens = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, want := reviewFixture()
			test.mutate(&result)
			data, _ := json.Marshal(result)
			if _, err := Parse(data, want); err == nil {
				t.Fatal("invalid review passed")
			}
		})
	}
	result, want := reviewFixture()
	result.Review.Checks[2].Outcome = "failed"
	data, _ := json.Marshal(result)
	parsed, err := Parse(data, want)
	if err != nil || parsed.Review.Passed() {
		t.Fatalf("test weakening must remain a failed verdict: %+v %v", parsed, err)
	}
}

func TestCandidateDigestBindsEveryPublishedOperationAndItsBase(t *testing.T) {
	candidate := Candidate{BaseRevision: strings.Repeat("a", 40), Changes: []Change{{Path: "src/a", Content: []byte("a"), Mode: "100644"}, {Path: "tests/b", Mode: "100644", Delete: true}}}
	digest := candidate.Digest()
	reordered := Candidate{BaseRevision: candidate.BaseRevision, Changes: []Change{candidate.Changes[1], candidate.Changes[0]}}
	if reordered.Digest() != digest {
		t.Fatal("operation order changed patch identity")
	}
	for _, mutate := range []func(*Candidate){
		func(c *Candidate) { c.BaseRevision = strings.Repeat("b", 40) },
		func(c *Candidate) { c.Changes[0].Content = []byte("different") },
		func(c *Candidate) { c.Changes[0].Mode = "100755" },
		func(c *Candidate) { c.Changes[1].Delete = false },
		func(c *Candidate) { c.Changes[0].Path = "src/other" },
	} {
		copy := Candidate{BaseRevision: candidate.BaseRevision, Changes: append([]Change{}, candidate.Changes...)}
		mutate(&copy)
		if copy.Digest() == digest {
			t.Fatal("changed publication kept an earlier review identity")
		}
	}
	text := "a"
	candidate.Changes[0].Text, candidate.Changes[0].Content = &text, nil
	if candidate.Digest() != digest {
		t.Fatal("readable text differs from the published bytes")
	}
}
