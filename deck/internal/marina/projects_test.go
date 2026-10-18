package marina

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectsFileMapsDeckProjectsToMarinaAndFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	write := func(body string) {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`[{"deck_project":"prj_default","namespace":"factory","project":"acceptance","workloads":{"repository/v2":"land-on-main"},"conversation_workload":"chat"}]`)
	mappings, err := LoadProjects(path)
	if err != nil || len(mappings) != 1 || mappings[0].Workloads["repository/v2"] != "land-on-main" || mappings[0].ConversationWorkload != "chat" {
		t.Fatalf("mappings = %+v, %v", mappings, err)
	}
	for _, invalid := range []string{
		`[{"deck_project":"p","namespace":"Factory","project":"acceptance"}]`,
		`[{"deck_project":"p","namespace":"factory","project":"acceptance","workloads":{"diagnostic-analysis/v1":"x"}}]`,
		`[{"deck_project":"p","namespace":"factory","project":"a"},{"deck_project":"p","namespace":"factory","project":"b"}]`,
		`[{"deck_project":"p","namespace":"factory","project":"acceptance","token":"x"}]`,
		`[] []`,
	} {
		write(invalid)
		if _, err := LoadProjects(path); err == nil {
			t.Errorf("accepted %s", invalid)
		}
	}
	if mappings, err := LoadProjects(""); err != nil || mappings != nil {
		t.Fatalf("no file = %v, %v", mappings, err)
	}
}
