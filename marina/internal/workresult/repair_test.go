package workresult

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRepairPlanRequiresDiagnosisAndApproachWithoutSourceChanges(t *testing.T) {
	for _, name := range []string{"valid", "no diagnosis", "no approach", "source changes", "no failure", "cannot repair"} {
		t.Run(name, func(t *testing.T) {
			plan := map[string]any{"diagnosis": "The boundary excludes the final value", "approach": "Handle the final value explicitly",
				"failure_digest": "sha256:" + strings.Repeat("b", 64), "can_repair": true}
			result := map[string]any{"stage": "repair_plan", "source_revision": strings.Repeat("a", 40),
				"observed_at": time.Now().UTC(), "tokens": 0, "summary": "Diagnosed regression", "repair": plan}
			switch name {
			case "no diagnosis":
				plan["diagnosis"] = " "
			case "no approach":
				plan["approach"] = " "
			case "source changes":
				result["changes"] = []map[string]any{{"path": "src/fix.go", "mode": "100644", "text": "unsafe"}}
			case "no failure":
				plan["failure_digest"] = ""
			case "cannot repair":
				plan["can_repair"] = false
				plan["approach"] = ""
			}
			data, _ := json.Marshal(result)
			_, err := Parse(data, Expected{Stage: "repair_plan", SourceRevision: strings.Repeat("a", 40)})
			valid := name == "valid" || name == "cannot repair"
			if valid && err != nil {
				t.Fatalf("valid diagnosis rejected: %v", err)
			}
			if !valid && err == nil {
				t.Fatal("invalid repair plan accepted")
			}
		})
	}
}
