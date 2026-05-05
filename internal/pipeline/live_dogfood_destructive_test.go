package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsDestructiveAtAuth covers the classifier's annotation-primary path,
// Cobra-leaf-segment fallback, read-only exemption, and negative cases.
// Cal.com's promoted command (Use="api-keys" with pp:endpoint=
// "api-keys.keys-refresh") is the motivating example: leaf-only matching
// misses it; the annotation lookup catches it.
func TestIsDestructiveAtAuth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		annotations map[string]string
		path        []string
		want        bool
	}{
		{
			name:        "annotation primary cal-com api-keys-refresh",
			annotations: map[string]string{"pp:endpoint": "api-keys.keys-refresh"},
			path:        []string{"cal-com-pp-cli", "api-keys"},
			want:        true,
		},
		{
			name:        "annotation primary token rotate",
			annotations: map[string]string{"pp:endpoint": "tokens.token-rotate"},
			path:        []string{"my-cli", "tokens"},
			want:        true,
		},
		{
			name:        "annotation primary regenerate",
			annotations: map[string]string{"pp:endpoint": "api-keys.regenerate-key"},
			path:        []string{"my-cli", "api-keys"},
			want:        true,
		},
		{
			name:        "annotation primary reset",
			annotations: map[string]string{"pp:endpoint": "passwords.reset-token"},
			path:        []string{"my-cli", "passwords"},
			want:        true,
		},
		{
			name:        "annotation primary cycle",
			annotations: map[string]string{"pp:endpoint": "tokens.cycle"},
			path:        []string{"my-cli", "tokens"},
			want:        true,
		},
		{
			name: "delete api-keys endpoint by method and path",
			annotations: map[string]string{
				"pp:endpoint": "api-keys.delete-key",
				"pp:method":   "DELETE",
				"pp:path":     "/v1/api-keys/{id}",
			},
			path: []string{"my-cli", "api-keys"},
			want: true,
		},
		{
			name: "delete sessions endpoint by method and path",
			annotations: map[string]string{
				"pp:endpoint": "sessions.destroy",
				"pp:method":   "DELETE",
				"pp:path":     "/sessions/{session_id}",
			},
			path: []string{"my-cli", "sessions"},
			want: true,
		},
		{
			name: "delete tokens endpoint by method and endpoint resource fallback",
			annotations: map[string]string{
				"pp:endpoint": "tokens.delete",
				"pp:method":   "DELETE",
			},
			path: []string{"my-cli", "tokens"},
			want: true,
		},
		{
			name: "delete ordinary resource is not destructive-at-auth",
			annotations: map[string]string{
				"pp:endpoint": "users.delete-user",
				"pp:method":   "DELETE",
				"pp:path":     "/users/{id}",
			},
			path: []string{"my-cli", "users"},
			want: false,
		},
		{
			name: "delete auth resource without DELETE method is not method-classified",
			annotations: map[string]string{
				"pp:endpoint": "api-keys.delete-preview",
				"pp:method":   "GET",
				"pp:path":     "/api-keys/{id}",
			},
			path: []string{"my-cli", "api-keys"},
			want: false,
		},
		{
			name:        "annotation case-insensitive",
			annotations: map[string]string{"pp:endpoint": "API-Keys.Refresh-Key"},
			path:        []string{"my-cli", "API-Keys"},
			want:        true,
		},
		{
			name:        "annotation present without destructive term",
			annotations: map[string]string{"pp:endpoint": "users.list-users"},
			path:        []string{"my-cli", "users", "refresh-cache"},
			want:        false,
		},
		{
			name: "leaf fallback novel command",
			path: []string{"my-cli", "auth", "refresh"},
			want: true,
		},
		{
			name: "leaf fallback compound name",
			path: []string{"my-cli", "oauth-clients", "users", "oauth-client-force-refresh"},
			want: true,
		},
		{
			name: "leaf fallback no match",
			path: []string{"my-cli", "users", "list"},
			want: false,
		},
		{
			name: "read-only exempt with pp:endpoint",
			annotations: map[string]string{
				"mcp:read-only": "true",
				"pp:endpoint":   "catalog.catalog-refresh",
			},
			path: []string{"craigslist-pp-cli", "catalog", "refresh"},
			want: false,
		},
		{
			name:        "read-only exempt leaf only",
			annotations: map[string]string{"mcp:read-only": "true"},
			path:        []string{"my-cli", "store", "refresh"},
			want:        false,
		},
		{
			name:        "destructive-auth false annotation exempts generated refresh helper",
			annotations: map[string]string{"pp:destructive-auth": "false"},
			path:        []string{"my-cli", "auth", "refresh-queries"},
			want:        false,
		},
		{
			name:        "destructive-auth true annotation opts in",
			annotations: map[string]string{"pp:destructive-auth": "true"},
			path:        []string{"my-cli", "auth", "metadata"},
			want:        true,
		},
		{
			name: "empty inputs",
			want: false,
		},
		{
			// Adversarial reviewer caught: a non-pp:endpoint annotation
			// containing a destructive term must not trigger the classifier.
			// Annotation-primary path reads pp:endpoint exclusively; other
			// keys (description, etc.) are not part of the contract.
			name: "destructive term in non-pp:endpoint key is ignored",
			annotations: map[string]string{
				"pp:endpoint": "users.list-users",
				"description": "list users (refresh the cache)",
			},
			path: []string{"my-cli", "users"},
			want: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := isDestructiveAtAuth(tt.annotations, tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}
