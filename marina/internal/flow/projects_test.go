package flow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestProjectConfigurationKeepsExplicitProfilesAndRejectsDuplicateBindings(t *testing.T) {
	data, err := os.ReadFile("../../../deploy/kubernetes/first-release/workflow.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var first Config
	if err = json.Unmarshal(data, &first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Checks = slices.Clone(first.Checks)
	second.ProjectID = "second-project"
	second.Git.Repository = "example/second"
	second.Git.CloneURL = "https://github.com/example/second.git"
	second.Git.TokenFile = "/secrets/second-git"
	second.Checks[0].ID = "second-check"
	path := filepath.Join(t.TempDir(), "projects.json")
	write := func(items []Config) {
		t.Helper()
		raw, _ := json.Marshal(struct {
			Version  int      `json:"version"`
			Projects []Config `json:"projects"`
		}{1, items})
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write([]Config{first, second})
	profiles, err := LoadProjects(path)
	if err != nil || len(profiles) != 2 || profiles[1].Git.TokenFile != second.Git.TokenFile {
		t.Fatalf("project profiles: %+v %v", profiles, err)
	}
	second.ProjectID = first.ProjectID
	write([]Config{first, second})
	if _, err = LoadProjects(path); err == nil {
		t.Fatal("duplicate project accepted")
	}
	second.ProjectID = "second-project"
	second.ArtifactDirectory = "/different-artifacts"
	write([]Config{first, second})
	if _, err = LoadProjects(path); err == nil {
		t.Fatal("inconsistent shared artifact route accepted")
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	profiles, err = LoadProjects(path)
	if err != nil || len(profiles) != 1 || profiles[0].Digest() != first.Digest() {
		t.Fatalf("legacy profile changed: %v", err)
	}
}
