// internal/store/migrate.go
package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// MaybeMigrateLegacy renames any pre-v2 SQLite file out of the way.
// We do NOT auto-import data — v2 introduces a richer phase schema that
// would be wrong to mechanically map. Users with existing projects run
// `cfcli migrate` (added in P1) explicitly.
func MaybeMigrateLegacy(dbPath string) error {
	legacyCandidates := []string{
		dbPath + ".sqlite",
		filepath.Join(filepath.Dir(dbPath), "store.sqlite"),
		filepath.Join(filepath.Dir(dbPath), "factory.db"),
		filepath.Join(filepath.Dir(dbPath), "state.db"),
	}
	for _, c := range legacyCandidates {
		if _, err := os.Stat(c); err != nil {
			continue
		}
		backup := c + ".legacy"
		if err := os.Rename(c, backup); err != nil {
			return fmt.Errorf("rename legacy %s -> %s: %w", c, backup, err)
		}
		fmt.Fprintf(os.Stderr, "chatbot-factory: legacy DB at %s renamed to %s. Run `cfcli migrate` to import.\n", c, backup)
	}
	return nil
}
