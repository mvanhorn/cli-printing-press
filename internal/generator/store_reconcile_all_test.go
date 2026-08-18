package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestReconcileAll_DeletesUnseenKeepsSeen(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("reconcile-all")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"things": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/things",
					Response: spec.ResponseDef{Type: "array", Item: "Thing"},
					IDField:  "id",
				},
				"get": {
					Method:   "GET",
					Path:     "/things/{thingId}",
					Response: spec.ResponseDef{Type: "object", Item: "Thing"},
					IDField:  "id",
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Thing": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "reconcile-all-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	require.Contains(t, string(storeSrc), "func (s *Store) ReconcileAll")

	inlineTest := `package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestReconcileAll_DeletesUnseenKeepsSeen(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	keep, err := json.Marshal(map[string]any{"id": "keep-1", "name": "keep"})
	if err != nil {
		t.Fatalf("marshal keep: %v", err)
	}
	stale, err := json.Marshal(map[string]any{"id": "stale-1", "name": "stale"})
	if err != nil {
		t.Fatalf("marshal stale: %v", err)
	}
	if _, _, err := s.UpsertBatch("things", []json.RawMessage{keep, stale}); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}

	deleted, err := s.ReconcileAll("things", []string{"keep-1"}, "things", nil)
	if err != nil {
		t.Fatalf("ReconcileAll: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("ReconcileAll deleted %d, want 1", deleted)
	}
	if _, err := s.Get("things", "keep-1"); err != nil {
		t.Fatalf("keep-1 missing after ReconcileAll: %v", err)
	}
	if _, err := s.Get("things", "stale-1"); err == nil {
		t.Fatal("stale-1 still present after ReconcileAll")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "store", "reconcile_all_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "./internal/store", "-run", "TestReconcileAll_DeletesUnseenKeepsSeen", "-count=1")
}
