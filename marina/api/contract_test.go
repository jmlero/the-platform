// Package api holds Marina's platform API contract (decision 0033). These tests
// keep the HTTP/JSON schemas, the custom resource definitions and the
// canonical fixtures in agreement. Run with -update to regenerate crds/.
package api

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "regenerate crds/ from openapi.yaml")

func loadOpenAPI(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func schemas(document map[string]any) map[string]any {
	return document["components"].(map[string]any)["schemas"].(map[string]any)
}

// resolve inlines every local $ref so a schema can be checked or embedded.
func resolve(all map[string]any, node any) any {
	switch value := node.(type) {
	case map[string]any:
		if ref, ok := value["$ref"].(string); ok {
			target := resolve(all, all[strings.TrimPrefix(ref, "#/components/schemas/")]).(map[string]any)
			merged := map[string]any{}
			for k, v := range target {
				merged[k] = v
			}
			for k, v := range value {
				if k != "$ref" {
					merged[k] = resolve(all, v)
				}
			}
			return merged
		}
		out := map[string]any{}
		for k, v := range value {
			out[k] = resolve(all, v)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, v := range value {
			out[i] = resolve(all, v)
		}
		return out
	}
	return node
}

func number(value any) (float64, bool) {
	switch n := value.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func typeMatches(kind string, value any) bool {
	switch kind {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		n, ok := value.(float64)
		return ok && n == float64(int64(n))
	case "number":
		_, ok := value.(float64)
		return ok
	}
	return false
}

// validate checks the JSON Schema subset this contract uses.
func validate(schema map[string]any, value any, path string) []string {
	var problems []string
	fail := func(format string, args ...any) { problems = append(problems, path+": "+fmt.Sprintf(format, args...)) }
	switch kind := schema["type"].(type) {
	case string:
		if !typeMatches(kind, value) {
			fail("want %s, got %T", kind, value)
			return problems
		}
	case []any:
		matched := false
		for _, k := range kind {
			matched = matched || typeMatches(k.(string), value)
		}
		if !matched {
			fail("want one of %v, got %T", kind, value)
			return problems
		}
	}
	if want, ok := schema["const"]; ok && !reflect.DeepEqual(want, value) {
		fail("want %v", want)
	}
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, option := range enum {
			found = found || reflect.DeepEqual(option, value)
		}
		if !found {
			fail("%v not in %v", value, enum)
		}
	}
	if text, ok := value.(string); ok {
		if pattern, ok := schema["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(text) {
			fail("%q does not match %s", text, pattern)
		}
		if limit, ok := number(schema["maxLength"]); ok && float64(len([]rune(text))) > limit {
			fail("longer than %v", limit)
		}
		if limit, ok := number(schema["minLength"]); ok && float64(len([]rune(text))) < limit {
			fail("shorter than %v", limit)
		}
		if schema["format"] == "date-time" {
			if _, err := time.Parse(time.RFC3339, text); err != nil {
				fail("not a date-time")
			}
		}
	}
	if n, ok := value.(float64); ok {
		if limit, ok := number(schema["minimum"]); ok && n < limit {
			fail("below %v", limit)
		}
		if limit, ok := number(schema["maximum"]); ok && n > limit {
			fail("above %v", limit)
		}
	}
	if items, ok := value.([]any); ok {
		if limit, ok := number(schema["maxItems"]); ok && float64(len(items)) > limit {
			fail("more than %v items", limit)
		}
		if limit, ok := number(schema["minItems"]); ok && float64(len(items)) < limit {
			fail("fewer than %v items", limit)
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range items {
				problems = append(problems, validate(itemSchema, item, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	}
	if object, ok := value.(map[string]any); ok {
		properties, _ := schema["properties"].(map[string]any)
		for _, name := range schema["required"].([]any) {
			if _, present := object[name.(string)]; !present {
				fail("missing %s", name)
			}
		}
		for key, field := range object {
			if propertySchema, ok := properties[key].(map[string]any); ok {
				problems = append(problems, validate(propertySchema, field, path+"."+key)...)
			} else if extra, ok := schema["additionalProperties"].(map[string]any); ok {
				problems = append(problems, validate(extra, field, path+"."+key)...)
			} else if schema["additionalProperties"] == false {
				fail("unknown field %s", key)
			}
		}
	}
	return problems
}

// requiredList gives every schema a required list so validate can range over it.
func requiredList(schema map[string]any) {
	if _, ok := schema["required"]; !ok {
		schema["required"] = []any{}
	}
	for _, v := range schema {
		switch child := v.(type) {
		case map[string]any:
			requiredList(child)
		case []any:
			for _, item := range child {
				if m, ok := item.(map[string]any); ok {
					requiredList(m)
				}
			}
		}
	}
}

var fixtures = map[string]string{
	"capabilities.json":           "Capabilities",
	"project.json":                "Project",
	"workload-repository.json":    "Workload",
	"workload-conversation.json":  "Workload",
	"run-created.json":            "Run",
	"run-succeeded.json":          "Run",
	"turn-succeeded.json":         "Turn",
	"decision-plan-approval.json": "Decision",
	"decision-answer.json":        "DecisionAnswer",
	"event-page.json":             "EventPage",
	"status-already-exists.json":  "Status",
}

func TestFixturesMatchTheirSchemas(t *testing.T) {
	all := schemas(loadOpenAPI(t))
	files, _ := filepath.Glob("fixtures/*.json")
	if len(files) != len(fixtures) {
		t.Fatalf("fixtures on disk %d, mapped %d", len(files), len(fixtures))
	}
	for file, name := range fixtures {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("fixtures", file))
			if err != nil {
				t.Fatal(err)
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			schema := resolve(all, all[name]).(map[string]any)
			requiredList(schema)
			if problems := validate(schema, value, name); len(problems) > 0 {
				t.Fatal(strings.Join(problems, "\n"))
			}
		})
	}
}

func TestSchemasRejectUnknownFieldsAndBadValues(t *testing.T) {
	all := schemas(loadOpenAPI(t))
	schema := resolve(all, all["Run"]).(map[string]any)
	requiredList(schema)
	raw, _ := os.ReadFile("fixtures/run-created.json")
	for name, change := range map[string]func(map[string]any){
		"unknown field":   func(run map[string]any) { run["spec"].(map[string]any)["engine"] = "x" },
		"missing budget":  func(run map[string]any) { delete(run["spec"].(map[string]any), "budget") },
		"bad name":        func(run map[string]any) { run["metadata"].(map[string]any)["name"] = "Task_42" },
		"attempts above":  func(run map[string]any) { run["spec"].(map[string]any)["budget"].(map[string]any)["maxAttempts"] = 99.0 },
		"bad digest":      func(run map[string]any) { run["spec"].(map[string]any)["context"].([]any)[0].(map[string]any)["digest"] = "md5:1" },
		"unknown trust":   func(run map[string]any) { run["spec"].(map[string]any)["context"].([]any)[0].(map[string]any)["trust"] = "admin" },
		"status by phase": func(run map[string]any) { run["status"] = map[string]any{"phase": "Done"} },
	} {
		t.Run(name, func(t *testing.T) {
			var run map[string]any
			_ = json.Unmarshal(raw, &run)
			change(run)
			if problems := validate(schema, run, "Run"); len(problems) == 0 {
				t.Fatal("invalid Run accepted")
			}
		})
	}
}

// The API never names the engine, sandbox or scheduling primitives.
func TestContractNamesNoEngineOrSandbox(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)\b(kagent|substrate|gvisor|harness|pods?|jobs?|codex)\b`)
	files, _ := filepath.Glob("fixtures/*.json")
	files = append(files, "openapi.yaml")
	crds, _ := filepath.Glob("crds/*.yaml")
	files = append(files, crds...)
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if match := forbidden.Find(raw); match != nil {
			t.Errorf("%s names %q", file, match)
		}
	}
}

type resource struct{ kind, plural string }

var resources = []resource{{"Project", "projects"}, {"Workload", "workloads"}, {"Run", "runs"}, {"Turn", "turns"}, {"Decision", "decisions"}}

// crdSchema converts an OpenAPI schema into a structural CRD schema.
func crdSchema(node any) any {
	switch value := node.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, v := range value {
			if k == "additionalProperties" && v == false {
				continue
			}
			out[k] = crdSchema(v)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, v := range value {
			out[i] = crdSchema(v)
		}
		return out
	}
	return node
}

func columns(kind string) []any {
	age := map[string]any{"name": "Age", "type": "date", "jsonPath": ".metadata.creationTimestamp"}
	switch kind {
	case "Run":
		return []any{map[string]any{"name": "Project", "type": "string", "jsonPath": ".spec.project"}, map[string]any{"name": "Phase", "type": "string", "jsonPath": ".status.phase"}, age}
	case "Turn":
		return []any{map[string]any{"name": "Phase", "type": "string", "jsonPath": ".status.phase"}, age}
	case "Decision":
		return []any{map[string]any{"name": "Kind", "type": "string", "jsonPath": ".spec.kind"}, map[string]any{"name": "Owner", "type": "string", "jsonPath": ".spec.owner.name"}, map[string]any{"name": "Phase", "type": "string", "jsonPath": ".status.phase"}, age}
	case "Workload":
		return []any{map[string]any{"name": "Type", "type": "string", "jsonPath": ".spec.type"}, age}
	}
	return []any{map[string]any{"name": "Repository", "type": "string", "jsonPath": ".spec.repository.url"}, age}
}

func definition(all map[string]any, r resource) map[string]any {
	singular := strings.ToLower(r.kind)
	spec := crdSchema(resolve(all, all[r.kind+"Spec"]))
	status := crdSchema(resolve(all, all[r.kind+"Status"]))
	return map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata":   map[string]any{"name": r.plural + ".marina.dev", "labels": map[string]any{"app.kubernetes.io/part-of": "the-platform"}},
		"spec": map[string]any{
			"group": "marina.dev",
			"scope": "Namespaced",
			"names": map[string]any{"kind": r.kind, "listKind": r.kind + "List", "plural": r.plural, "singular": singular, "categories": []any{"marina"}},
			"versions": []any{map[string]any{
				"name": "v1", "served": true, "storage": true,
				"subresources":             map[string]any{"status": map[string]any{}},
				"additionalPrinterColumns": columns(r.kind),
				"schema": map[string]any{"openAPIV3Schema": map[string]any{
					"type":     "object",
					"required": []any{"spec"},
					"properties": map[string]any{
						"apiVersion": map[string]any{"type": "string"},
						"kind":       map[string]any{"type": "string"},
						"metadata":   map[string]any{"type": "object"},
						"spec":       spec,
						"status":     status,
					},
				}},
			}},
		},
	}
}

func TestCustomResourceDefinitionsAreGeneratedFromTheSchemas(t *testing.T) {
	all := schemas(loadOpenAPI(t))
	for _, r := range resources {
		path := filepath.Join("crds", r.plural+".yaml")
		encoded, err := yaml.Marshal(definition(all, r))
		if err != nil {
			t.Fatal(err)
		}
		generated := append([]byte("# Generated from openapi.yaml by: go test ./api -update. Do not edit.\n"), encoded...)
		if *update {
			if err := os.MkdirAll("crds", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, generated, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		current, err := os.ReadFile(path)
		if err != nil || string(current) != string(generated) {
			t.Errorf("%s is out of date; run go test ./api -update", path)
		}
	}
}

// Every object fixture also decodes as its custom resource: the same document
// passes the generated CRD schema for spec and status.
func TestObjectFixturesAreValidCustomResources(t *testing.T) {
	all := schemas(loadOpenAPI(t))
	kinds := map[string]bool{}
	for _, r := range resources {
		kinds[r.kind] = true
	}
	names := make([]string, 0, len(fixtures))
	for file := range fixtures {
		names = append(names, file)
	}
	sort.Strings(names)
	for _, file := range names {
		kind := fixtures[file]
		if !kinds[kind] {
			continue
		}
		raw, _ := os.ReadFile(filepath.Join("fixtures", file))
		var object map[string]any
		_ = json.Unmarshal(raw, &object)
		for _, part := range []string{"spec", "status"} {
			if value, ok := object[part]; ok {
				schema := resolve(all, all[kind+strings.Title(part)]).(map[string]any)
				requiredList(schema)
				if problems := validate(schema, value, file+"."+part); len(problems) > 0 {
					t.Error(strings.Join(problems, "\n"))
				}
			}
		}
	}
}
