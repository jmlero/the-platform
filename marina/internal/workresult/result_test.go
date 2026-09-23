package workresult

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestParseRejectsFalseRepositoryResults(t *testing.T) {
	now := time.Now().UTC()
	head := strings.Repeat("a", 40)
	checks := []Check{{ID: "test", Directory: ".", Command: []string{"go", "test", "./..."}}}
	base := Result{Stage: "verify", SourceRevision: head, ObservedAt: now, Checks: []CheckResult{{ID: "test", Directory: ".", Command: checks[0].Command, ExitCode: 0, Output: "ok", OutputDigest: artifacts.Digest([]byte("ok"))}}}
	expect := Expected{Stage: "verify", SourceRevision: head, Checks: checks, NotBefore: now.Add(-time.Minute), Now: now}
	for name, mutate := range map[string]func(*Result){
		"wrong revision":    func(r *Result) { r.SourceRevision = strings.Repeat("b", 40) },
		"missing check":     func(r *Result) { r.Checks = nil },
		"wrong command":     func(r *Result) { r.Checks[0].Command = []string{"true"} },
		"fabricated output": func(r *Result) { r.Checks[0].OutputDigest = artifacts.Digest([]byte("other")) },
		"extra change":      func(r *Result) { r.Changes = []Change{{Path: "app.go", Mode: "100644"}} },
		"future time":       func(r *Result) { r.ObservedAt = now.Add(2 * time.Minute) },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			r.Checks = append([]CheckResult(nil), base.Checks...)
			mutate(&r)
			b, _ := json.Marshal(r)
			if _, err := Parse(b, expect); err == nil {
				t.Fatal("false verification result accepted")
			}
		})
	}
	b, _ := json.Marshal(base)
	if _, err := Parse(b, expect); err != nil {
		t.Fatalf("valid result: %v", err)
	}
}

func TestParseRejectsDisallowedOrUnboundedChanges(t *testing.T) {
	head := strings.Repeat("a", 40)
	base := Result{Stage: "implement", SourceRevision: head, ObservedAt: time.Now().UTC(), Changes: []Change{{Path: "app/main.go", Mode: "100644", Content: []byte("package main")}}}
	expect := Expected{Stage: "implement", SourceRevision: head, AllowedPaths: []string{"app"}}
	for name, mutate := range map[string]func(*Result){
		"outside scope": func(r *Result) { r.Changes[0].Path = "other/main.go" },
		"control file":  func(r *Result) { r.Changes[0].Path = "app/AGENTS.md" },
		"duplicate":     func(r *Result) { r.Changes = append(r.Changes, r.Changes[0]) },
		"invalid mode":  func(r *Result) { r.Changes[0].Mode = "120000" },
		"over limit":    func(r *Result) { r.Changes[0].Content = []byte(strings.Repeat("x", 2<<20+1)) },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			r.Changes = append([]Change(nil), base.Changes...)
			mutate(&r)
			b, _ := json.Marshal(r)
			if _, err := Parse(b, expect); err == nil {
				t.Fatal("unsafe implementation accepted")
			}
		})
	}
	b, _ := json.Marshal(base)
	if _, err := Parse(b, expect); err != nil {
		t.Fatalf("valid changes: %v", err)
	}
}

func TestA2AResultRequiresBoundedInlineBytes(t *testing.T) {
	head := strings.Repeat("a", 40)
	result := Result{Stage: "implement", SourceRevision: head, ObservedAt: time.Now().UTC(), Changes: []Change{{Path: "app/main.go", Mode: "100644", Content: []byte("safe")}}}
	b, _ := json.Marshal(result)
	expect := Expected{Stage: "implement", SourceRevision: head, AllowedPaths: []string{"app"}}
	artifact := &a2a.Artifact{Name: "marina-result.json", Parts: []*a2a.Part{{MediaType: "application/json", Content: &a2a.Part_Raw{Raw: b}}}}
	encode := func() []byte { data, _ := protojson.Marshal(artifact); return data }
	if _, err := ParseA2A(encode(), expect); err != nil {
		t.Fatalf("valid inline artifact: %v", err)
	}
	artifact.Parts[0].Content = &a2a.Part_Url{Url: "https://example.invalid/result.json"}
	if _, err := ParseA2A(encode(), expect); err == nil {
		t.Fatal("URL-only artifact accepted")
	}
	artifact.Parts[0].Content = &a2a.Part_Text{Text: string(b)}
	if _, err := ParseA2A(encode(), expect); err == nil {
		t.Fatal("text claim accepted")
	}
	artifact.Parts[0].Content = &a2a.Part_Raw{Raw: b}
	artifact.Name = "unrelated.json"
	if _, err := ParseA2A(encode(), expect); err == nil {
		t.Fatal("unselected artifact accepted")
	}
	if _, err := ParseA2A([]byte(strings.Repeat("x", MaxResultBytes+1)), expect); err == nil {
		t.Fatal("oversized engine artifact accepted")
	}
	artifact.Name = ""
	artifact.Parts = []*a2a.Part{{Content: &a2a.Part_Text{Text: string(b[:len(b)/2])}}, {Content: &a2a.Part_Text{Text: string(b[len(b)/2:])}}}
	if _, err := ParseA2A(encode(), expect); err != nil {
		t.Fatalf("native Codex text result: %v", err)
	}
	artifact.Parts[1].Content = &a2a.Part_Url{Url: "https://example.invalid/result.json"}
	if _, err := ParseA2A(encode(), expect); err == nil {
		t.Fatal("mixed text and URL artifact accepted")
	}
}
