package pipeline

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := NewState("fst", "telegram")
	s.Phases["init"] = PhaseState{Status: "completed", Plan: "completed", CompletedAt: time.Now().UTC()}
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.Project != "fst" || got.Channel != "telegram" {
		t.Fatalf("project/channel mismatch")
	}
	if got.Phases["init"].Status != "completed" {
		t.Fatalf("init phase not preserved")
	}
	if len(got.Phases) != len(PhaseOrder) {
		t.Fatalf("expected %d phases, got %d", len(PhaseOrder), len(got.Phases))
	}
}

func TestNextPending(t *testing.T) {
	s := NewState("fst", "telegram")
	s.Phases["preflight"] = PhaseState{Status: "completed"}
	s.Phases["init"] = PhaseState{Status: "completed"}
	got := s.NextPending()
	if got != "chunk" {
		t.Fatalf("want chunk, got %s", got)
	}
}
