package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFilterVerifyEnv pins the contract that the two verify env vars
// (PRINTING_PRESS_VERIFY and PRINTING_PRESS_VERIFY_LIVE_HTTP) are stripped
// from subprocess env, and every other env var survives. Failure mode if
// dropped: live verifiers silently noop on destructive paths when an
// operator has PRINTING_PRESS_VERIFY=1 inherited.
func TestFilterVerifyEnv(t *testing.T) {
	t.Parallel()

	t.Run("strips both verify vars", func(t *testing.T) {
		in := []string{
			"PATH=/usr/bin:/bin",
			"PRINTING_PRESS_VERIFY=1",
			"HOME=/home/op",
			"PRINTING_PRESS_VERIFY_LIVE_HTTP=1",
			"PETSTORE_TOKEN=abc123",
		}
		out := filterVerifyEnv(in)
		assert.NotContains(t, out, "PRINTING_PRESS_VERIFY=1")
		assert.NotContains(t, out, "PRINTING_PRESS_VERIFY_LIVE_HTTP=1")
		assert.Contains(t, out, "PATH=/usr/bin:/bin")
		assert.Contains(t, out, "HOME=/home/op")
		assert.Contains(t, out, "PETSTORE_TOKEN=abc123")
		assert.Len(t, out, 3, "exactly the 2 verify vars should be dropped, leaving 3 of 5")
	})

	t.Run("strips verify vars regardless of value", func(t *testing.T) {
		// Any value, not just "1" — operator might have stale ="0" or
		// "true" or empty. Strip on key match.
		in := []string{
			"PRINTING_PRESS_VERIFY=",
			"PRINTING_PRESS_VERIFY=true",
			"PRINTING_PRESS_VERIFY_LIVE_HTTP=0",
		}
		out := filterVerifyEnv(in)
		assert.Empty(t, out, "all verify-keyed entries should be stripped regardless of value")
	})

	t.Run("does not strip entries whose values contain the var names", func(t *testing.T) {
		// Defense against substring-match bugs: a real env var like
		// CUSTOM_NOTE="see PRINTING_PRESS_VERIFY docs" must survive.
		in := []string{
			"CUSTOM_NOTE=see PRINTING_PRESS_VERIFY docs",
			"PRINTING_PRESS_VERIFY_OTHER=should-survive",
			"OTHER=PRINTING_PRESS_VERIFY_LIVE_HTTP=1",
		}
		out := filterVerifyEnv(in)
		assert.Len(t, out, 3, "exact-key match must not drop entries whose values mention the var names")
		assert.Contains(t, out, "CUSTOM_NOTE=see PRINTING_PRESS_VERIFY docs")
		assert.Contains(t, out, "PRINTING_PRESS_VERIFY_OTHER=should-survive")
		assert.Contains(t, out, "OTHER=PRINTING_PRESS_VERIFY_LIVE_HTTP=1")
	})

	t.Run("handles malformed entries gracefully", func(t *testing.T) {
		// Entries without '=' are unusual but should pass through, not crash.
		in := []string{"PATH=/usr/bin", "MALFORMED_NO_EQUALS", "PRINTING_PRESS_VERIFY=1"}
		out := filterVerifyEnv(in)
		assert.Contains(t, out, "PATH=/usr/bin")
		assert.Contains(t, out, "MALFORMED_NO_EQUALS")
		assert.NotContains(t, out, "PRINTING_PRESS_VERIFY=1")
	})

	t.Run("empty input", func(t *testing.T) {
		out := filterVerifyEnv(nil)
		assert.Empty(t, out)
	})
}
