package domain

import "testing"

func TestPRDRequiresBoundedMarkdownInsidePRDDirectory(t *testing.T) {
	draft := PRDDraft{Title: "Filters", Path: "docs/prd/filters.md", Content: "# Filters\n"}
	if err := draft.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside.md", "docs/prd/../../AGENTS.md", "/docs/prd/filters.md", "docs/prd/../filters.md", "docs/prd/filter.md\n", "docs/prd\\filters.md", "docs/prd/script.sh"} {
		draft.Path = name
		if draft.Validate() == nil {
			t.Errorf("accepted unsafe path %q", name)
		}
	}
}
