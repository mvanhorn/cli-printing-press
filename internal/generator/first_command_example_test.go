package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/stretchr/testify/assert"
)

// TestFirstCommandExampleHonorsPromotion covers issue #290. The Wikipedia
// CLI's spec has a single-endpoint `feed` resource (`feed.get-on-this-day`),
// which the generator promotes to a top-level `feed` command. The example
// helper used to return `feed get-on-this-day` (the pre-promotion path) for
// the SKILL.md profile-example block, which the printing-press-library
// `Verify SKILL.md` workflow rejected because that command path doesn't
// exist in the shipped CLI.
func TestFirstCommandExampleHonorsPromotion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resources map[string]spec.Resource
		want      string
	}{
		{
			name: "single-endpoint resource gets promoted, example returns just resource name",
			resources: map[string]spec.Resource{
				"feed": {
					Endpoints: map[string]spec.Endpoint{
						"get-on-this-day": {Method: "GET", Path: "/feed/onthisday"},
					},
				},
			},
			want: "feed",
		},
		{
			name: "multi-endpoint resource with preferred verb returns resource + verb",
			resources: map[string]spec.Resource{
				"items": {
					Endpoints: map[string]spec.Endpoint{
						"list":   {Method: "GET", Path: "/items"},
						"create": {Method: "POST", Path: "/items"},
					},
				},
			},
			want: "items list",
		},
		{
			name: "multi-endpoint resource without preferred verb falls back to alphabetically first",
			resources: map[string]spec.Resource{
				"items": {
					Endpoints: map[string]spec.Endpoint{
						"create":   {Method: "POST", Path: "/items"},
						"register": {Method: "POST", Path: "/items/register"},
					},
				},
			},
			want: "items create",
		},
		{
			name: "single-endpoint resource named after a builtin is not promoted; emits resource + endpoint",
			resources: map[string]spec.Resource{
				"version": {
					Endpoints: map[string]spec.Endpoint{
						"info": {Method: "GET", Path: "/version/info"},
					},
				},
			},
			want: "version info",
		},
		{
			name: "single-endpoint resource whose only endpoint is a preferred verb emits just resource name",
			resources: map[string]spec.Resource{
				"reports": {
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/reports"},
					},
				},
			},
			want: "reports",
		},
		{
			name: "preferred-verb match in any resource wins over alphabetically-first fallback",
			resources: map[string]spec.Resource{
				"alpha": {
					Endpoints: map[string]spec.Endpoint{
						"unusual-name": {Method: "GET", Path: "/alpha"},
					},
				},
				"beta": {
					Endpoints: map[string]spec.Endpoint{
						"list":   {Method: "GET", Path: "/beta"},
						"delete": {Method: "DELETE", Path: "/beta/{id}"},
					},
				},
			},
			want: "beta list",
		},
		{
			name:      "empty resources returns empty string",
			resources: map[string]spec.Resource{},
			want:      "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, firstCommandExample(tc.resources))
		})
	}
}
