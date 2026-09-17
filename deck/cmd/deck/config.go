package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Fail closed instead of ignoring an existing authentication policy or ledger.
// The previous spelling is used only to diagnose installations needing upgrade.
func validateRenamedConfiguration(environ []string, dataDir string) error {
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "APPDECK_") {
			return fmt.Errorf("rename environment variable %s to %s before starting Deck", name, strings.Replace(name, "APPDECK_", "DECK_", 1))
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := filepath.Join(dataDir, "appdeck.db"+suffix)
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("previous ledger file %s exists; stop both services and follow docs/decisions/0012-deck-component-name.md before starting Deck", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect previous ledger: %w", err)
		}
	}
	return nil
}
