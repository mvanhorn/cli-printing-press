// internal/store/envvars.go
package store

import (
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func envKey(slug, name string) []byte {
	return []byte(slug + "/" + name)
}

// SetEnv stores an env value for a project. Empty values delete.
func (s *Store) SetEnv(slug, name, value string) error {
	if slug == "" || name == "" {
		return fmt.Errorf("slug and name required")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketEnvVars))
		if value == "" {
			return b.Delete(envKey(slug, name))
		}
		return b.Put(envKey(slug, name), []byte(value))
	})
}

// GetEnv reads a single env value.
func (s *Store) GetEnv(slug, name string) (string, error) {
	var out string
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(bucketEnvVars)).Get(envKey(slug, name))
		out = string(raw)
		return nil
	})
	return out, err
}

// ListEnv returns all env vars for a project.
func (s *Store) ListEnv(slug string) (map[string]string, error) {
	out := map[string]string{}
	prefix := []byte(slug + "/")
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket([]byte(bucketEnvVars)).Cursor()
		for k, v := c.Seek(prefix); k != nil && hasPrefix(k, prefix); k, v = c.Next() {
			out[string(k[len(prefix):])] = string(v)
		}
		return nil
	})
	return out, err
}
