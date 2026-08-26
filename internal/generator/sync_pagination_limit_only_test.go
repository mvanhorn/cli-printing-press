// Copyright 2026 Anthropic, PBC. Licensed under Apache-2.0.

package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// limitOnlyPaginationSpec is the cursorless shape that used to copy a
// declared page-size default into sync, capping the local mirror at that
// many rows with no way to advance.
func limitOnlyPaginationSpec(name string, defaultLimit int, maximum *float64) *spec.APISpec {
	apiSpec := minimalSpec(name)
	param := spec.Param{Name: "limit", Type: "integer", Default: defaultLimit}
	if maximum != nil {
		param.Maximum = maximum
	}
	apiSpec.Resources = map[string]spec.Resource{
		"computers": {
			Description: "Computers",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/api/v1/computers",
					Params:   []spec.Param{param},
					Response: spec.ResponseDef{Type: "array"},
				},
			},
		},
	}
	return apiSpec
}

// TestGeneratedSyncLimitOnlyIgnoresPageSizeDefault proves the emitted
// determinePaginationDefaults does not copy a spec-declared limit default
// onto a cursorless resource. The assertion runs the generated helper so it
// is compile-level, not a comment or template-source check.
func TestGeneratedSyncLimitOnlyIgnoresPageSizeDefault(t *testing.T) {
	t.Parallel()

	apiSpec := limitOnlyPaginationSpec("limonly", 25, nil)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	runtimeTest := `package cli

import "testing"

func TestDeterminePaginationDefaultsLimitOnlyIgnoresDefault(t *testing.T) {
	if !resourceSupportsPagination("computers") {
		t.Fatal("resourceSupportsPagination(\"computers\") = false, want true so sync still attempts a paged fetch")
	}
	got := determinePaginationDefaults("computers")
	if got.limitParam != "limit" {
		t.Fatalf("limitParam = %q, want \"limit\"", got.limitParam)
	}
	if got.cursorParam != "" {
		t.Fatalf("cursorParam = %q, want empty for a limit-only resource", got.cursorParam)
	}
	if got.limit != 100 {
		t.Fatalf("limit = %d, want 100 (generator default), not the spec-declared page-size default of 25", got.limit)
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "sync_limit_only_page_size_test.go"),
		[]byte(runtimeTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestDeterminePaginationDefaultsLimitOnlyIgnoresDefault")
}

func TestGeneratedSyncLimitOnlyPrefersDeclaredMaximum(t *testing.T) {
	t.Parallel()

	max500 := 500.0
	apiSpec := limitOnlyPaginationSpec("limax", 25, &max500)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	runtimeTest := `package cli

import "testing"

func TestDeterminePaginationDefaultsLimitOnlyPrefersMaximum(t *testing.T) {
	got := determinePaginationDefaults("computers")
	if got.limit != 500 {
		t.Fatalf("limit = %d, want 500 (declared maximum on a cursorless resource), not the spec default of 25 or the generator default of 100", got.limit)
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "sync_limit_only_max_page_size_test.go"),
		[]byte(runtimeTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestDeterminePaginationDefaultsLimitOnlyPrefersMaximum")
}
