package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedStoreListZeroLimitReturnsAllRows(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("store-list-contract")
	outputDir := filepath.Join(t.TempDir(), "store-list-contract-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testPath := filepath.Join(outputDir, "internal", "store", "list_contract_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func TestListZeroLimitReturnsAllRows(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	for i := 0; i < 250; i++ {
		raw, err := json.Marshal(map[string]any{"id": fmt.Sprintf("item-%03d", i)})
		if err != nil {
			t.Fatalf("marshal row %d: %v", i, err)
		}
		if err := db.Upsert("items", fmt.Sprintf("item-%03d", i), raw); err != nil {
			t.Fatalf("upsert row %d: %v", i, err)
		}
	}

	allRows, err := db.List("items", 0)
	if err != nil {
		t.Fatalf("list all rows: %v", err)
	}
	if len(allRows) != 250 {
		t.Fatalf("List with zero limit returned %d rows, want 250", len(allRows))
	}

	limitedRows, err := db.List("items", 50)
	if err != nil {
		t.Fatalf("list limited rows: %v", err)
	}
	if len(limitedRows) != 50 {
		t.Fatalf("List with positive limit returned %d rows, want 50", len(limitedRows))
	}
}
	`), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestListZeroLimitReturnsAllRows", "-count=1")
}
