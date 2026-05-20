// internal/store/store.go
package store

import (
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

const (
	bucketProjects = "projects"
	bucketPhases   = "phases"
	bucketEnvVars  = "envvars"
	bucketMeta     = "meta"
)

// Store is the persistent key-value store for the CLI.
type Store struct {
	db *bolt.DB
}

// Open opens or creates the database at the given path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, b := range []string{bucketProjects, bucketPhases, bucketEnvVars, bucketMeta} {
			if _, err := tx.CreateBucketIfNotExists([]byte(b)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DefaultPath returns the default DB location: $HOME/.chatbot-factory/store.db
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("default store path: %w", err)
	}
	return filepath.Join(home, ".chatbot-factory", "store.db"), nil
}

// hasPrefix checks if k starts with the given prefix.
func hasPrefix(k, prefix []byte) bool {
	if len(k) < len(prefix) {
		return false
	}
	for i := range prefix {
		if k[i] != prefix[i] {
			return false
		}
	}
	return true
}
