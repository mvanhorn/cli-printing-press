package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaleWorkflowMergesSpecTimestampFields(t *testing.T) {
	t.Parallel()

	t.Run("adds spec date-time fields", func(t *testing.T) {
		t.Parallel()

		apiSpec := minimalSpec("stalespecfields")
		apiSpec.Types = map[string]spec.TypeDef{
			"Card": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "dateLastActivity", Type: "string", Format: "date-time"},
				},
			},
		}

		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		gen := New(apiSpec, outputDir)
		gen.VisionSet = VisionTemplateSet{
			Store: true,
			Sync:  true,
			Workflows: []string{
				"workflows/pm_stale.go.tmpl",
			},
		}
		require.NoError(t, gen.Generate())

		staleSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "pm_stale.go"))
		require.NoError(t, err)
		src := string(staleSrc)

		assert.Contains(t, src, "func init()")
		assert.Contains(t, src, `"dateLastActivity"`)
	})

	t.Run("dedups spec date-time field already in the hardcoded allowlist", func(t *testing.T) {
		t.Parallel()

		// updated_at is already hardcoded; declaring it as a spec date-time
		// field must not produce a duplicate entry after the init() merge.
		apiSpec := minimalSpec("stalespecdedup")
		apiSpec.Types = map[string]spec.TypeDef{
			"Item": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "updated_at", Type: "string", Format: "date-time"},
				},
			},
		}

		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		gen := New(apiSpec, outputDir)
		gen.VisionSet = VisionTemplateSet{
			Store: true,
			Sync:  true,
			Workflows: []string{
				"workflows/pm_stale.go.tmpl",
			},
		}
		require.NoError(t, gen.Generate())

		// Behavioral: after init() runs, updated_at appears exactly once.
		testPath := filepath.Join(outputDir, "internal", "cli", "pm_stale_dedup_test.go")
		require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import "testing"

func TestStaleTimestampFieldsDedup(t *testing.T) {
	n := 0
	for _, f := range staleTimestampFields {
		if f == "updated_at" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("updated_at must appear exactly once after init merge, got %d", n)
	}
}
`), 0o644))
		runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestStaleTimestampFieldsDedup", "-count=1")
	})

	t.Run("keeps hardcoded fields and emits no init when no spec date-time names", func(t *testing.T) {
		t.Parallel()

		apiSpec := minimalSpec("stalespecdefaults")
		apiSpec.Types = map[string]spec.TypeDef{
			"Item": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "updated_at", Type: "string"},
				},
			},
		}

		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		gen := New(apiSpec, outputDir)
		gen.VisionSet = VisionTemplateSet{
			Store: true,
			Sync:  true,
			Workflows: []string{
				"workflows/pm_stale.go.tmpl",
			},
		}
		require.NoError(t, gen.Generate())

		staleSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "pm_stale.go"))
		require.NoError(t, err)
		src := string(staleSrc)

		for _, field := range []string{
			"updatedAt",
			"updated_at",
			"updatedDate",
			"modifiedAt",
			"modified_at",
			"lastModified",
			"last_modified",
			"lastEditedTime",
			"last_edited_time",
			"updated",
			"last_updated",
		} {
			assert.Contains(t, src, `"`+field+`"`)
		}
		assert.False(t, strings.Contains(src, "func init()"), "pm_stale.go should omit init when no date-time fields were collected from the spec")
	})
}
