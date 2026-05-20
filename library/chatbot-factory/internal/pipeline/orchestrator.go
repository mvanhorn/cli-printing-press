package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Orchestrator struct {
	StatePath  string
	JSONOutput bool
}

func NewOrchestrator(statePath string) *Orchestrator {
	return &Orchestrator{StatePath: statePath}
}

type progressEvent struct {
	Event     string   `json:"event"`
	Phase     string   `json:"phase"`
	Project   string   `json:"project"`
	Error     string   `json:"error,omitempty"`
	Artifacts []string `json:"artifacts,omitempty"`
}

func (o *Orchestrator) emit(ev progressEvent) {
	if !o.JSONOutput {
		return
	}
	buf, _ := json.Marshal(ev)
	fmt.Fprintln(os.Stdout, string(buf))
}

func (o *Orchestrator) RunNext(ctx context.Context) error {
	state, err := LoadState(o.StatePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	next := state.NextPending()
	if next == "" {
		return nil
	}
	return o.runPhase(ctx, state, next)
}

func (o *Orchestrator) RunAll(ctx context.Context) error {
	for {
		state, err := LoadState(o.StatePath)
		if err != nil {
			return err
		}
		next := state.NextPending()
		if next == "" {
			return nil
		}
		if err := o.runPhase(ctx, state, next); err != nil {
			return err
		}
	}
}

func (o *Orchestrator) RunPhase(ctx context.Context, name string) error {
	state, err := LoadState(o.StatePath)
	if err != nil {
		return err
	}
	return o.runPhase(ctx, state, name)
}

func (o *Orchestrator) runPhase(ctx context.Context, state *State, name string) error {
	phase, err := Get(name)
	if err != nil {
		return err
	}
	rc := RunContext{
		Ctx:        ctx,
		Project:    state.Project,
		ProjectDir: filepath.Dir(filepath.Dir(o.StatePath)),
		State:      state,
		JSONOutput: o.JSONOutput,
		StatePath:  o.StatePath,
	}

	state.MarkRunning(name)
	if err := state.Save(o.StatePath); err != nil {
		return fmt.Errorf("save running state: %w", err)
	}
	o.emit(progressEvent{Event: "phase_start", Phase: name, Project: state.Project})

	artifacts, runErr := phase.Run(rc)
	if runErr != nil {
		state.MarkFailed(name, runErr)
		_ = state.Save(o.StatePath)
		o.emit(progressEvent{Event: "phase_failed", Phase: name, Project: state.Project, Error: runErr.Error()})
		return runErr
	}

	if gateErr := phase.Gate(rc); gateErr != nil {
		state.MarkFailed(name, fmt.Errorf("gate failed: %w", gateErr))
		_ = state.Save(o.StatePath)
		o.emit(progressEvent{Event: "phase_failed", Phase: name, Project: state.Project, Error: gateErr.Error()})
		return gateErr
	}

	state.MarkCompleted(name, artifacts)
	if err := state.Save(o.StatePath); err != nil {
		return err
	}
	o.emit(progressEvent{Event: "phase_done", Phase: name, Project: state.Project, Artifacts: artifacts})
	return nil
}
