package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsDestructiveAtAuthAnnotationPrimary covers the cal-com case from
// #602: a promoted command with Use="api-keys" but pp:endpoint annotation
// "api-keys.keys-refresh". Leaf-path matching alone misses this; the
// classifier MUST read the annotation.
func TestIsDestructiveAtAuthAnnotationPrimary(t *testing.T) {
	t.Parallel()

	got := isDestructiveAtAuth(
		map[string]string{"pp:endpoint": "api-keys.keys-refresh"},
		[]string{"cal-com-pp-cli", "api-keys"},
	)
	assert.True(t, got, "annotation pp:endpoint=api-keys.keys-refresh must classify as destructive-at-auth")
}

func TestIsDestructiveAtAuthAnnotationRotate(t *testing.T) {
	t.Parallel()

	got := isDestructiveAtAuth(
		map[string]string{"pp:endpoint": "tokens.token-rotate"},
		[]string{"my-cli", "tokens"},
	)
	assert.True(t, got)
}

func TestIsDestructiveAtAuthLeafFallbackNovelCommand(t *testing.T) {
	t.Parallel()

	// No annotation (novel hand-built command); leaf segment is `refresh`.
	got := isDestructiveAtAuth(nil, []string{"my-cli", "auth", "refresh"})
	assert.True(t, got, "annotation absent — leaf-segment match should fire")
}

func TestIsDestructiveAtAuthLeafFallbackCompoundName(t *testing.T) {
	t.Parallel()

	// Compound leaf name like cal-com's `oauth-client-force-refresh`. Substring
	// match (not exact) catches it.
	got := isDestructiveAtAuth(
		nil,
		[]string{"my-cli", "oauth-clients", "users", "oauth-client-force-refresh"},
	)
	assert.True(t, got)
}

func TestIsDestructiveAtAuthReadOnlyExempt(t *testing.T) {
	t.Parallel()

	// craigslist `catalog refresh` is annotated mcp:read-only=true. It cannot
	// rotate auth regardless of name.
	got := isDestructiveAtAuth(
		map[string]string{
			"mcp:read-only": "true",
			"pp:endpoint":   "catalog.catalog-refresh",
		},
		[]string{"craigslist-pp-cli", "catalog", "refresh"},
	)
	assert.False(t, got, "mcp:read-only=true must exempt regardless of name")
}

func TestIsDestructiveAtAuthReadOnlyExemptLeafOnly(t *testing.T) {
	t.Parallel()

	// Read-only annotation must also short-circuit the leaf-fallback path.
	got := isDestructiveAtAuth(
		map[string]string{"mcp:read-only": "true"},
		[]string{"my-cli", "store", "refresh"},
	)
	assert.False(t, got)
}

func TestIsDestructiveAtAuthCaseInsensitive(t *testing.T) {
	t.Parallel()

	got := isDestructiveAtAuth(
		map[string]string{"pp:endpoint": "API-Keys.Refresh-Key"},
		[]string{"my-cli", "API-Keys"},
	)
	assert.True(t, got)
}

func TestIsDestructiveAtAuthAnnotationPresentNoSignal(t *testing.T) {
	t.Parallel()

	// Annotation present but doesn't contain destructive terms — and the
	// leaf-fallback is intentionally suppressed when pp:endpoint is set
	// (the annotation is authoritative for endpoint-mirror commands).
	got := isDestructiveAtAuth(
		map[string]string{"pp:endpoint": "users.list-users"},
		[]string{"my-cli", "users", "refresh-cache"},
	)
	assert.False(t, got, "pp:endpoint without destructive term should not fall back to leaf — annotation is authoritative")
}

func TestIsDestructiveAtAuthNegativeNoSignal(t *testing.T) {
	t.Parallel()

	got := isDestructiveAtAuth(
		map[string]string{"pp:endpoint": "users.list-users"},
		[]string{"my-cli", "users"},
	)
	assert.False(t, got)
}

func TestIsDestructiveAtAuthNegativeNoAnnotationNoMatch(t *testing.T) {
	t.Parallel()

	got := isDestructiveAtAuth(nil, []string{"my-cli", "users", "list"})
	assert.False(t, got)
}

func TestIsDestructiveAtAuthEmptyInputs(t *testing.T) {
	t.Parallel()

	assert.False(t, isDestructiveAtAuth(nil, nil))
	assert.False(t, isDestructiveAtAuth(map[string]string{}, []string{}))
}
