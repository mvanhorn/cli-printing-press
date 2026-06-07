package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedStoreListLimitSemantics(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("store-list-limit")
	outputDir := filepath.Join(t.TempDir(), "store-list-limit-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testPath := filepath.Join(outputDir, "internal", "store", "store_list_limit_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package store

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestListLimitSemantics(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	for i := 0; i < 250; i++ {
		id := fmt.Sprintf("item-%03d", i)
		if err := db.Upsert("items", id, []byte(fmt.Sprintf(`+"`"+`{"id":%q}`+"`"+`, id))); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	allRows, err := db.List("items", 0)
	if err != nil {
		t.Fatalf("list all rows: %v", err)
	}
	if len(allRows) != 250 {
		t.Fatalf("List(items, 0) returned %d rows, want all 250", len(allRows))
	}

	negativeRows, err := db.List("items", -1)
	if err != nil {
		t.Fatalf("list all rows with negative limit: %v", err)
	}
	if len(negativeRows) != 250 {
		t.Fatalf("List(items, -1) returned %d rows, want all 250", len(negativeRows))
	}

	cappedRows, err := db.List("items", 50)
	if err != nil {
		t.Fatalf("list capped rows: %v", err)
	}
	if len(cappedRows) != 50 {
		t.Fatalf("List(items, 50) returned %d rows, want 50", len(cappedRows))
	}
}
`), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/store", "-run", "^TestListLimitSemantics$", "-count=1")
}
