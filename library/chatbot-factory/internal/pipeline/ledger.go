package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Finding struct {
	ID             string                 `json:"id"`
	Kind           string                 `json:"kind"`
	Note           string                 `json:"note,omitempty"`
	Status         string                 `json:"status,omitempty"`
	DecisionFields map[string]interface{} `json:"decision_fields,omitempty"`
	seenThisRun    bool                   `json:"-"`
}

type LedgerProgress struct {
	LastProcessedFindingID string `json:"last_processed_finding_id,omitempty"`
}

type Ledger struct {
	SchemaVersion int                `json:"schema_version"`
	Phase         string             `json:"phase"`
	UpdatedAt     time.Time          `json:"updated_at"`
	Progress      LedgerProgress     `json:"progress"`
	Findings      map[string]Finding `json:"findings"`
}

func NewLedger(phase string) *Ledger {
	return &Ledger{
		SchemaVersion: 1,
		Phase:         phase,
		Findings:      map[string]Finding{},
	}
}

func LoadLedger(path string) (*Ledger, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l Ledger
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("parse ledger: %w", err)
	}
	if l.Findings == nil {
		l.Findings = map[string]Finding{}
	}
	return &l, nil
}

func (l *Ledger) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	l.UpdatedAt = time.Now().UTC()
	buf, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (l *Ledger) BeginRun() {
	for k, f := range l.Findings {
		f.seenThisRun = false
		l.Findings[k] = f
	}
}

func (l *Ledger) UpsertFinding(f Finding) {
	cur, ok := l.Findings[f.ID]
	if !ok {
		f.Status = "pending"
		f.seenThisRun = true
		l.Findings[f.ID] = f
		return
	}
	cur.Kind = f.Kind
	cur.seenThisRun = true
	l.Findings[f.ID] = cur
}

func (l *Ledger) EndRun() {
	for k, f := range l.Findings {
		if !f.seenThisRun {
			delete(l.Findings, k)
		}
	}
}

func (l *Ledger) AcceptFinding(id, note string, fields map[string]interface{}) error {
	f, ok := l.Findings[id]
	if !ok {
		return fmt.Errorf("finding %s not found", id)
	}
	if note == "" {
		return fmt.Errorf("note required to accept")
	}
	f.Status = "accepted"
	f.Note = note
	if fields != nil {
		f.DecisionFields = fields
	}
	l.Findings[id] = f
	return nil
}

func (l *Ledger) CountPending() int {
	n := 0
	for _, f := range l.Findings {
		if f.Status == "" || f.Status == "pending" {
			n++
		}
	}
	return n
}
