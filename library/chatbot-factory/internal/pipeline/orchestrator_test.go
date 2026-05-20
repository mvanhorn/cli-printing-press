package pipeline

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type fakePhase struct {
	name    string
	runErr  error
	gateErr error
	ran     bool
	gated   bool
}

func (f *fakePhase) Name() string                        { return f.name }
func (f *fakePhase) Run(rc RunContext) ([]string, error) { f.ran = true; return nil, f.runErr }
func (f *fakePhase) Gate(rc RunContext) error            { f.gated = true; return f.gateErr }

func TestOrchestratorRunNext(t *testing.T) {
	registry = map[string]Phase{}
	for _, name := range PhaseOrder {
		Register(&fakePhase{name: name})
	}
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	s := NewState("fst", "telegram")
	_ = s.Save(statePath)

	o := NewOrchestrator(statePath)
	if err := o.RunNext(context.Background()); err != nil {
		t.Fatalf("RunNext: %v", err)
	}
	reloaded, _ := LoadState(statePath)
	if reloaded.Phases["preflight"].Status != "completed" {
		t.Fatalf("expected preflight completed, got %s", reloaded.Phases["preflight"].Status)
	}
}

func TestOrchestratorGateFailMarksFailed(t *testing.T) {
	registry = map[string]Phase{}
	for _, name := range PhaseOrder {
		p := &fakePhase{name: name}
		if name == "preflight" {
			p.gateErr = errors.New("gate boom")
		}
		Register(p)
	}
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	s := NewState("fst", "telegram")
	_ = s.Save(statePath)

	o := NewOrchestrator(statePath)
	err := o.RunNext(context.Background())
	if err == nil {
		t.Fatal("expected error from gate")
	}
	reloaded, _ := LoadState(statePath)
	if reloaded.Phases["preflight"].Status != "failed" {
		t.Fatalf("expected failed, got %s", reloaded.Phases["preflight"].Status)
	}
}
