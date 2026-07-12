package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
)

// t.TempDir() returns a $HOME-rooted path on macOS (/var/folders/.../) and
// /tmp on Linux, so the redaction has a real absolute prefix to strip.
func TestWriteLedger_RedactsHomePaths(t *testing.T) {
	cliDir := t.TempDir()

	if err := writeLedger(cliDir, nil, nil, nil); err != nil {
		t.Fatalf("writeLedger: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cliDir, artifacts.ToolsPolishLedgerFilename))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	s := string(data)
	for _, prefix := range []string{`"/Users/`, `"/home/`, `"C:\\Users\\`, `"/var/folders/`, `"/tmp/`} {
		if strings.Contains(s, prefix) {
			t.Fatalf("ledger must not contain %q prefix; got:\n%s", prefix, s)
		}
	}

	var loaded ToolsAuditLedger
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal ledger: %v", err)
	}
	want := filepath.Join(artifacts.CLIDirPlaceholder, filepath.Base(cliDir))
	if loaded.CLIDir != want {
		t.Fatalf("ledger CLIDir = %q, want %q", loaded.CLIDir, want)
	}
}
