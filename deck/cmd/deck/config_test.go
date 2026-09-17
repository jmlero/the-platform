package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartupRequiresRenamedConfigurationWithoutExposingValues(t *testing.T) {
	err := validateRenamedConfiguration([]string{"APPDECK_OPERATOR_AUTH_FILE=private-value"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "DECK_OPERATOR_AUTH_FILE") || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("configuration error = %v", err)
	}
}

func TestStartupRequiresExplicitMoveOfExistingDatabase(t *testing.T) {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		t.Run(suffix, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "appdeck.db"+suffix)
			if err := os.WriteFile(path, []byte("retained"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := validateRenamedConfiguration(nil, dir); err == nil {
				t.Fatal("startup would hide the existing ledger")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "retained" {
				t.Fatalf("existing data changed: %q, %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "deck.db")); !os.IsNotExist(err) {
				t.Fatalf("new database created: %v", err)
			}
		})
	}
}

func TestStartupAcceptsDeckConfiguration(t *testing.T) {
	if err := validateRenamedConfiguration([]string{"DECK_DEMO_AGENT=false"}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
