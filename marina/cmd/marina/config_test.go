package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurationCheckIsOfflineAndOmitsPrivateContent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "workflow.json")
	if err := os.WriteFile(path, []byte(`{"model":{"token_file":"private-credential-file"},"analysis":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"check-config", "--config", path}, &stdout, &stderr); code != 1 {
		t.Fatalf("invalid configuration returned %d: %s", code, stderr.String())
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), "private-credential-file") || strings.Contains(stderr.String(), `"analysis"`) {
		t.Fatalf("configuration content exposed: %s %s", &stdout, &stderr)
	}
}

func TestExampleConfigurationSelectsSupportedNativeWorkloads(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check-config", "--config", "../../../deploy/kubernetes/first-release/workflow.example.json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("example configuration returned %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "configuration_digest") || stderr.Len() != 0 {
		t.Fatalf("validation summary: %s %s", &stdout, &stderr)
	}
}
