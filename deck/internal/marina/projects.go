package marina

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
)

const maxProjectsBytes = 1 << 20

// ProfileIDs are the Deck workload profiles a mapping can bind. repository/v2
// lands on main; repository/v1 opens a pull request.
var ProfileIDs = []string{"repository/v1", "repository/v2"}

// ProjectMapping binds one Deck project to a Marina Project and the Marina
// Workloads that execute its tasks and chat turns.
type ProjectMapping struct {
	DeckProject          string            `json:"deck_project"`
	Namespace            string            `json:"namespace"`
	Project              string            `json:"project"`
	Workloads            map[string]string `json:"workloads"`
	ConversationWorkload string            `json:"conversation_workload,omitempty"`
}

// LoadProjects reads and validates the mapping file. It is reread on every
// cycle so a mounted ConfigMap can change without a restart; an invalid file
// fails closed.
func LoadProjects(path string) ([]ProjectMapping, error) {
	if path == "" {
		return nil, nil
	}
	contents, err := readBounded(path, maxProjectsBytes)
	if err != nil {
		return nil, fmt.Errorf("read Marina projects file: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var mappings []ProjectMapping
	if err := decoder.Decode(&mappings); err != nil {
		return nil, fmt.Errorf("decode Marina projects file: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("Marina projects file has trailing data")
	}
	seen := map[string]bool{}
	for _, m := range mappings {
		if m.DeckProject == "" || len(m.DeckProject) > 128 || seen[m.DeckProject] {
			return nil, fmt.Errorf("each mapping needs a distinct deck_project")
		}
		seen[m.DeckProject] = true
		if !ValidName(m.Namespace) || !ValidName(m.Project) {
			return nil, fmt.Errorf("mapping %s needs a valid Marina namespace and project", m.DeckProject)
		}
		for profile, workload := range m.Workloads {
			if !slices.Contains(ProfileIDs, profile) || !ValidName(workload) {
				return nil, fmt.Errorf("mapping %s binds an unknown profile or invalid workload", m.DeckProject)
			}
		}
		if m.ConversationWorkload != "" && !ValidName(m.ConversationWorkload) {
			return nil, fmt.Errorf("mapping %s has an invalid conversation workload", m.DeckProject)
		}
	}
	return mappings, nil
}

func mappingFor(mappings []ProjectMapping, project string) (ProjectMapping, bool) {
	for _, m := range mappings {
		if m.DeckProject == project {
			return m, true
		}
	}
	return ProjectMapping{}, false
}
