// internal/store/phases.go
package store

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// PhaseState captures the execution state of a single pipeline phase.
type PhaseState struct {
	Status      string    `json:"status"`
	Plan        string    `json:"plan"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Artifacts   []string  `json:"artifacts,omitempty"`
	Ledger      string    `json:"ledger,omitempty"`
	Error       string    `json:"error,omitempty"`
}

func phaseKey(slug, phase string) []byte {
	return []byte(slug + "/" + phase)
}

// SetPhase upserts a phase row.
func (s *Store) SetPhase(slug, phase string, st PhaseState) error {
	if slug == "" || phase == "" {
		return fmt.Errorf("slug and phase required")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		buf, err := json.Marshal(st)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketPhases)).Put(phaseKey(slug, phase), buf)
	})
}

// GetPhase loads a phase row. Returns (PhaseState{}, nil) if not found.
func (s *Store) GetPhase(slug, phase string) (PhaseState, error) {
	var out PhaseState
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(bucketPhases)).Get(phaseKey(slug, phase))
		if raw == nil {
			return nil
		}
		return json.Unmarshal(raw, &out)
	})
	return out, err
}

// ListPhases returns all phases for a project as a name→state map.
func (s *Store) ListPhases(slug string) (map[string]PhaseState, error) {
	out := map[string]PhaseState{}
	prefix := []byte(slug + "/")
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket([]byte(bucketPhases)).Cursor()
		for k, v := c.Seek(prefix); k != nil && hasPrefix(k, prefix); k, v = c.Next() {
			var st PhaseState
			if err := json.Unmarshal(v, &st); err != nil {
				return err
			}
			phaseName := string(k[len(prefix):])
			out[phaseName] = st
		}
		return nil
	})
	return out, err
}
