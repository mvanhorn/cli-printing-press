// Copyright 2026 Anthropic, PBC. Licensed under Apache-2.0.

package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// paginationMaximumSpec builds a spec whose list endpoint declares a page_size
// param with an inclusive maximum below the 100 default. It exercises the
// full profiler -> template path so the assertion is on the *generated* CLI's
// behavior, not on a hand-constructed profile.
func paginationMaximumSpec(name string, maximum float64) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Resources = map[string]spec.Resource{
		"widgets": {
			Description: "Widgets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method: "GET",
					Path:   "/widgets",
					Params: []spec.Param{
						{Name: "cursor", Type: "string"},
						{Name: "page_size", Type: "int", Maximum: &maximum},
					},
					Response:   spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{Type: "cursor", LimitParam: "page_size", CursorParam: "cursor"},
				},
			},
		},
	}
	return apiSpec
}

// TestGeneratedSyncPaginationCapsAtSpecMaximum proves the emitted
// determinePaginationDefaults caps the page size at the spec's declared
// page_size maximum instead of the 100 default. Without the clamp, sync sends
// limit=100 and APIs that cap page_size below 100 reject the request (e.g.
// HTTP 400 "Number must be less than or equal to 30"). The assertion is
// compile-level: it runs the generated helper and reads its return value,
// so it survives gofmt spacing and struct-literal reordering.
func TestGeneratedSyncPaginationCapsAtSpecMaximum(t *testing.T) {
	t.Parallel()

	apiSpec := paginationMaximumSpec("pagcap", 30)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	runtimeTest := `package cli

import "testing"

func TestDeterminePaginationDefaultsCapsAtSpecMaximum(t *testing.T) {
	// resourceSupportsPagination must be true, otherwise the capped limit is
	// never actually sent and the cap would be vacuously "correct".
	if !resourceSupportsPagination("widgets") {
		t.Fatal("resourceSupportsPagination(\"widgets\") = false, want true so the capped limit is sent")
	}
	got := determinePaginationDefaults("widgets")
	if got.limitParam != "page_size" {
		t.Fatalf("limitParam = %q, want \"page_size\"", got.limitParam)
	}
	if got.limit != 30 {
		t.Fatalf("limit = %d, want 30 (the spec's page_size maximum, not the 100 default)", got.limit)
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "sync_pagination_cap_test.go"),
		[]byte(runtimeTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestDeterminePaginationDefaultsCapsAtSpecMaximum")
}

// TestGeneratedSyncSinceRendersUTC pins the incremental-sync temporal filter to
// UTC. A local-zone RFC3339 renders a numeric offset like "-04:00", which some
// APIs reject as an invalid date; ".UTC().Format" renders the zone as "Z".
func TestGeneratedSyncSinceRendersUTC(t *testing.T) {
	t.Parallel()

	apiSpec := paginationMaximumSpec("sinceutc", 30)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")

	// Both since-formatting sites (explicit --since and last_synced_at
	// incremental) must render in UTC.
	assert.Contains(t, syncSrc, "ts.UTC().Format(time.RFC3339)",
		"explicit --since must be formatted in UTC")
	assert.Contains(t, syncSrc, "lastSynced.UTC().Format(time.RFC3339)",
		"incremental last_synced_at must be formatted in UTC")

	// The bare local-zone forms must be gone.
	assert.NotContains(t, syncSrc, "ts.Format(time.RFC3339)",
		"explicit --since must not use the bare local-zone Format")
	assert.NotContains(t, syncSrc, "lastSynced.Format(time.RFC3339)",
		"incremental since must not use the bare local-zone Format")
}
