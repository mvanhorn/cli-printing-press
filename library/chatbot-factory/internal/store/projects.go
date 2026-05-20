// internal/store/projects.go
package store

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Project is the persisted form of a chatbot project.
type Project struct {
	Slug      string    `json:"slug"`
	Channel   string    `json:"channel"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UpsertProject creates or replaces a project record.
func (s *Store) UpsertProject(p Project) error {
	if p.Slug == "" {
		return fmt.Errorf("slug required")
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	return s.db.Update(func(tx *bolt.Tx) error {
		buf, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketProjects)).Put([]byte(p.Slug), buf)
	})
}

// GetProject loads a project by slug. Returns (Project{}, nil) if not found.
func (s *Store) GetProject(slug string) (Project, error) {
	var p Project
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(bucketProjects)).Get([]byte(slug))
		if raw == nil {
			return nil
		}
		return json.Unmarshal(raw, &p)
	})
	return p, err
}

// ListProjects returns all projects sorted by slug (bbolt iteration is sorted by key).
func (s *Store) ListProjects() ([]Project, error) {
	var out []Project
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketProjects)).ForEach(func(_, v []byte) error {
			var p Project
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			out = append(out, p)
			return nil
		})
	})
	return out, err
}

// DeleteProject removes a project and its phase/env data.
func (s *Store) DeleteProject(slug string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketProjects))
		if b.Get([]byte(slug)) == nil {
			return fmt.Errorf("project %q not found", slug)
		}
		if err := b.Delete([]byte(slug)); err != nil {
			return err
		}
		for _, bucket := range []string{bucketPhases, bucketEnvVars} {
			b := tx.Bucket([]byte(bucket))
			c := b.Cursor()
			prefix := []byte(slug + "/")
			for k, _ := c.Seek(prefix); k != nil && hasPrefix(k, prefix); k, _ = c.Next() {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
