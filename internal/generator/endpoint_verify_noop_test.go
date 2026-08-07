package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndpoint_VerifyNoopPropagation pins that when Client.do()
// short-circuits a mutating verb under PRINTING_PRESS_VERIFY mode and the
// inner data payload carries the reserved __pp_verify_synthetic__
// sentinel, the emitted endpoint handler surfaces that signal at the
// OUTER envelope level via verify_noop: true and flips success: false.
// Without this, naive validators keying on .success or .status read a
// noop as a real mutation because the synthetic envelope returns HTTP 200.
func TestEndpoint_VerifyNoopPropagation(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("verify-noop")
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Manage items",
		Endpoints: map[string]spec.Endpoint{
			"list":   {Method: "GET", Path: "/items", Description: "List items"},
			"delete": {Method: "DELETE", Path: "/items/{id}", Description: "Delete one item"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "verify-noop-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "items_delete.go"))
	require.NoError(t, err)
	emitted := string(src)

	// Sentinel-driven detection: handler must check parsed data for the
	// reserved __pp_verify_synthetic__ field name.
	assert.Contains(t, emitted, `"__pp_verify_synthetic__"`,
		"endpoint handler should detect the synthetic envelope via __pp_verify_synthetic__")

	// Top-level signal: when the inner sentinel is present, the OUTER
	// envelope carries verify_noop: true.
	assert.Contains(t, emitted, `envelope["verify_noop"] = true`,
		"endpoint handler should set top-level verify_noop=true on synthetic envelope detection")

	// Success-flip: a noop did not actually mutate. Mirrors the dry_run
	// shape that lives in the same envelope block.
	assert.Contains(t, emitted, `envelope["success"] = false`,
		"endpoint handler should flip success=false on synthetic envelope detection")

	// Ordering invariant: detection MUST run against RAW data (before the
	// `filtered := data` assignment) so the sentinel field cannot be
	// stripped by --compact / --select before detection sees it. The
	// verify_noop assignment must appear BEFORE the `filtered := data`
	// line, and the envelope marshal must follow.
	verifyNoop := strings.Index(emitted, `envelope["verify_noop"] = true`)
	filteredAssign := strings.Index(emitted, `filtered := data`)
	marshal := strings.Index(emitted, `envelopeJSON, err := json.Marshal(envelope)`)
	require.NotEqual(t, -1, verifyNoop, "envelope[\"verify_noop\"] = true must exist")
	require.NotEqual(t, -1, filteredAssign, "filtered := data assignment must exist")
	require.NotEqual(t, -1, marshal, "envelopeJSON marshal call must exist")
	assert.True(t, verifyNoop < filteredAssign,
		"verify_noop detection must precede filtered := data so --compact/--select cannot strip the sentinel before detection")
	assert.True(t, filteredAssign < marshal,
		"filtered assignment must precede envelope marshal so populated data field makes it into output")

	// Detection runs against RAW bytes — unmarshal target should be `data`
	// (not `filtered`). A regression that swaps these silently re-opens
	// the --compact bypass.
	assert.Contains(t, emitted, "json.Unmarshal(data, &rawParsed)",
		"sentinel detection must unmarshal RAW data, not filtered")

	// GET handlers must NOT carry the mutating-verb envelope block at all
	// — verify the list handler is unaffected.
	listSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "items_list.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(listSrc), `envelope["verify_noop"] = true`,
		"GET handlers should not emit the verify_noop propagation block")
}
