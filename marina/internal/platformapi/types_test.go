package platformapi

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The Go types round-trip every canonical object fixture without losing or
// inventing a field.
func TestTypesRoundTripTheContractFixtures(t *testing.T) {
	for file, target := range map[string]any{
		"project.json": &Project{}, "workload-repository.json": &Workload{}, "workload-conversation.json": &Workload{},
		"run-created.json": &Run{}, "run-succeeded.json": &Run{}, "turn-succeeded.json": &Turn{},
		"decision-plan-approval.json": &Decision{}, "event-page.json": &EventPage{}, "capabilities.json": &Capabilities{},
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "api", "fixtures", file))
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(target); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		encoded, _ := json.Marshal(target)
		var before, after any
		_ = json.Unmarshal(raw, &before)
		_ = json.Unmarshal(encoded, &after)
		left, _ := json.Marshal(before)
		right, _ := json.Marshal(after)
		if !bytes.Equal(left, right) {
			t.Errorf("%s changed in a round trip:\n%s\n%s", file, left, right)
		}
	}
}
