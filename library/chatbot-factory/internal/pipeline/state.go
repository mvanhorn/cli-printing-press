package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const SchemaVersion = 2

// PhaseState mirrors store.PhaseState but lives in JSON-on-disk per project.
type PhaseState struct {
	Status      string    `json:"status"`
	Plan        string    `json:"plan"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Artifacts   []string  `json:"artifacts,omitempty"`
	Ledger      string    `json:"ledger,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// State is the per-project pipeline state, persisted at
// `<project-dir>/.chatbot-factory/state.json`.
type State struct {
	SchemaVersion       int                   `json:"schema_version"`
	Project             string                `json:"project"`
	Channel             string                `json:"channel"`
	Phases              map[string]PhaseState `json:"phases"`
	CredentialsProvided []string              `json:"credentials_provided,omitempty"`
}

// NewState returns a State with all phases initialized to pending.
func NewState(slug, channel string) *State {
	phases := make(map[string]PhaseState, len(PhaseOrder))
	for _, p := range PhaseOrder {
		phases[p] = PhaseState{Status: "pending", Plan: "seed"}
	}
	return &State{
		SchemaVersion: SchemaVersion,
		Project:       slug,
		Channel:       channel,
		Phases:        phases,
	}
}

// LoadState reads state.json from disk.
func LoadState(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse state.json: %w", err)
	}
	if s.Phases == nil {
		s.Phases = map[string]PhaseState{}
	}
	for _, p := range PhaseOrder {
		if _, ok := s.Phases[p]; !ok {
			s.Phases[p] = PhaseState{Status: "pending", Plan: "seed"}
		}
	}
	return &s, nil
}

// Save atomically writes state.json.
func (s *State) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	buf, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// NextPending returns the first non-completed, non-skipped phase. Returns "" when done.
func (s *State) NextPending() string {
	for _, p := range PhaseOrder {
		st := s.Phases[p]
		if st.Status != "completed" && st.Status != "skipped" {
			return p
		}
	}
	return ""
}

func (s *State) MarkRunning(phase string) {
	st := s.Phases[phase]
	st.Status = "executing"
	st.StartedAt = time.Now().UTC()
	st.Error = ""
	s.Phases[phase] = st
}

func (s *State) MarkCompleted(phase string, artifacts []string) {
	st := s.Phases[phase]
	st.Status = "completed"
	st.Plan = "completed"
	st.CompletedAt = time.Now().UTC()
	if artifacts != nil {
		st.Artifacts = artifacts
	}
	s.Phases[phase] = st
}

func (s *State) MarkFailed(phase string, err error) {
	st := s.Phases[phase]
	st.Status = "failed"
	if err != nil {
		st.Error = err.Error()
	}
	s.Phases[phase] = st
}

func (s *State) MarkSkipped(phase, reason string) {
	st := s.Phases[phase]
	st.Status = "skipped"
	if reason != "" {
		st.Error = "skipped: " + reason
	}
	s.Phases[phase] = st
}
