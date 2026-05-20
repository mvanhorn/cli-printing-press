// internal/store/store_test.go
package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestProjectsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	p := Project{Slug: "fst", Channel: "telegram"}
	if err := s.UpsertProject(p); err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	got, err := s.GetProject("fst")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Slug != "fst" || got.Channel != "telegram" {
		t.Fatalf("unexpected project: %+v", got)
	}
	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1, got %d", len(list))
	}
}

func TestPhasesRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertProject(Project{Slug: "fst", Channel: "telegram"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPhase("fst", "init", PhaseState{Status: "completed"}); err != nil {
		t.Fatalf("SetPhase: %v", err)
	}
	ph, err := s.GetPhase("fst", "init")
	if err != nil {
		t.Fatalf("GetPhase: %v", err)
	}
	if ph.Status != "completed" {
		t.Fatalf("want completed, got %s", ph.Status)
	}
}

func TestEnvVarsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetEnv("fst", "OPENROUTER_API_KEY", "sk-or-abc"); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}
	v, err := s.GetEnv("fst", "OPENROUTER_API_KEY")
	if err != nil {
		t.Fatalf("GetEnv: %v", err)
	}
	if v != "sk-or-abc" {
		t.Fatalf("want sk-or-abc, got %q", v)
	}
}

func TestDeleteProjectCascades(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertProject(Project{Slug: "fst", Channel: "telegram"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPhase("fst", "init", PhaseState{Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnv("fst", "KEY", "val"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject("fst"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	phases, err := s.ListPhases("fst")
	if err != nil {
		t.Fatal(err)
	}
	if len(phases) != 0 {
		t.Fatalf("phases not deleted, got %d", len(phases))
	}
	envs, err := s.ListEnv("fst")
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 0 {
		t.Fatalf("envs not deleted, got %d", len(envs))
	}
}

func TestDeleteProjectNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteProject("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}
