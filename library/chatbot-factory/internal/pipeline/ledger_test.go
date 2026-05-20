package pipeline

import (
	"path/filepath"
	"testing"
)

func TestLedgerPersistAcceptsAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".audit-ledger.json")

	l := NewLedger("audit")
	l.UpsertFinding(Finding{ID: "h-1", Kind: "hardcoded-phone"})
	l.UpsertFinding(Finding{ID: "h-2", Kind: "hardcoded-phone"})
	l.UpsertFinding(Finding{ID: "h-3", Kind: "hardcoded-phone"})
	if err := l.AcceptFinding("h-2", "intentional fallback", nil); err != nil {
		t.Fatal(err)
	}
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}

	l2, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if l2 == nil {
		t.Fatal("nil ledger")
	}
	l2.BeginRun()
	l2.UpsertFinding(Finding{ID: "h-1", Kind: "hardcoded-phone"})
	l2.UpsertFinding(Finding{ID: "h-3", Kind: "hardcoded-phone"})
	l2.EndRun()

	if l2.Findings["h-1"].Status == "accepted" {
		t.Fatal("h-1 should not be accepted")
	}
	if _, ok := l2.Findings["h-2"]; ok {
		t.Fatal("h-2 should have been removed")
	}
}
