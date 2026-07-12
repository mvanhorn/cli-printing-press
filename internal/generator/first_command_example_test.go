package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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
		{
			// recipes has only one endpoint (get) so the resource is
			// promoted: the cobra path is just "recipes <slug>", not
			// "recipes get <slug>". Spec author's Param.Default for
			// the positional wins over canonicalargs.
			name: "single-endpoint promoted resource with positional spec default",
			resources: map[string]spec.Resource{
				"recipes": {
					Endpoints: map[string]spec.Endpoint{
						"get": {
							Method: "GET",
							Path:   "/recipes/{slug}",
							Params: []spec.Param{
								{Name: "slug", Positional: true, Default: "my-best-brownies"},
							},
						},
					},
				},
			},
			want: "recipes my-best-brownies",
		},
		{
			// Two endpoints — no promotion — and `since` is a
			// canonicalargs entry, so positional value comes from there.
			name: "multi-endpoint resource positional falls through to canonicalargs",
			resources: map[string]spec.Resource{
				"changelog": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET",
							Path:   "/changelog",
							Params: []spec.Param{
								{Name: "since", Positional: true},
							},
						},
						"reset": {Method: "POST", Path: "/changelog/reset"},
					},
				},
			},
			want: "changelog list 2026-01-01",
		},
		{
			// Two endpoints — no promotion. The positional has no spec
			// default and no canonicalargs entry, so falls through to
			// the mock-value catch-all. Mirrors the lookup chain in
			// internal/pipeline/runtime_commands.go.
			name: "multi-endpoint resource positional falls through to mock-value",
			resources: map[string]spec.Resource{
				"airports": {
					Endpoints: map[string]spec.Endpoint{
						"get": {
							Method: "GET",
							Path:   "/airports/{code}",
							Params: []spec.Param{
								{Name: "airport_code", Positional: true},
							},
						},
						"create": {Method: "POST", Path: "/airports"},
					},
				},
			},
			want: "airports get mock-value",
		},
		{
			// articles has two endpoints (browse-sub and list) so the
			// resource is NOT promoted; example emits "articles browse-sub <pos1> <pos2>".
			// list is selected over browse-sub because it's a preferred verb,
			// so to test multi-positional we add a multi-positional list.
			name: "multiple positionals are joined in declared order",
			resources: map[string]spec.Resource{
				"articles": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET",
							Path:   "/articles/{vertical}/{sub}",
							Params: []spec.Param{
								{Name: "vertical", Positional: true},
								{Name: "sub", Positional: true, Default: "weeknight"},
							},
						},
						"create": {Method: "POST", Path: "/articles"},
					},
				},
			},
			want: "articles list mock-vertical weeknight",
		},
		{
			// items has two endpoints — no promotion. Optional
			// non-positional flag params don't appear.
			name: "optional non-positional params do not pollute the example",
			resources: map[string]spec.Resource{
				"items": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET",
							Path:   "/items",
							Params: []spec.Param{
								{Name: "limit", Positional: false, Default: 25},
								{Name: "cursor", Positional: false},
							},
						},
						"create": {Method: "POST", Path: "/items"},
					},
				},
			},
			want: "items list",
		},
		{
			name: "required non-positional params use public flag names",
			resources: map[string]spec.Resource{
				"stores": {
					Endpoints: map[string]spec.Endpoint{
						"find": {
							Method: "GET",
							Path:   "/stores",
							Params: []spec.Param{
								{Name: "s", FlagName: "address", Required: true, Type: "string"},
								{Name: "c", FlagName: "city", Required: true, Type: "string"},
							},
						},
						"refresh": {Method: "POST", Path: "/stores/refresh"},
					},
				},
			},
			want: "stores find --address example-value --city example-value",
		},
		{
			name: "required non-positional params without public names use derived flag names",
			resources: map[string]spec.Resource{
				"search": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET",
							Path:   "/search",
							Params: []spec.Param{
								{Name: "search_query", Required: true, Type: "string"},
							},
						},
						"refresh": {Method: "POST", Path: "/search/refresh"},
					},
				},
			},
			want: "search list --search-query example-value",
		},
		{
			name: "required dispatch type param keeps string default",
			resources: map[string]spec.Resource{
				"domain": {
					Endpoints: map[string]spec.Endpoint{
						"rank": {
							Method: "GET",
							Path:   "/",
							Params: []spec.Param{
								{Name: "type", Required: true, Type: "string", Default: "domain_rank"},
								{Name: "domain", Required: true, Type: "string"},
							},
						},
						"refresh": {Method: "POST", Path: "/domain/refresh"},
					},
				},
			},
			want: "domain rank --type domain_rank --domain example-value",
		},
		{
			name: "explicit dispatch param keeps string default",
			resources: map[string]spec.Resource{
				"reports": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET",
							Path:   "/reports",
							Params: []spec.Param{
								{Name: "mode", Required: true, Type: "string", Default: "summary", DispatchParam: true},
							},
						},
						"refresh": {Method: "POST", Path: "/reports/refresh"},
					},
				},
			},
			want: "reports list --mode summary",
		},
		{
			name: "explicit dispatch false suppresses action heuristic",
			resources: map[string]spec.Resource{
				"jobs": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET",
							Path:   "/jobs",
							Params: []spec.Param{
								{Name: "action", Required: true, Type: "string", Default: "create", DispatchParamSet: true},
							},
						},
						"refresh": {Method: "POST", Path: "/jobs/refresh"},
					},
				},
			},
			want: "jobs list --action example-value",
		},
		{
			name: "path placeholder sharing query param name does not keep default",
			resources: map[string]spec.Resource{
				"reports": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET",
							Path:   "/items/{mode}/reports",
							Params: []spec.Param{
								{Name: "mode", Required: true, Type: "string", Default: "summary"},
							},
						},
						"refresh": {Method: "POST", Path: "/reports/refresh"},
					},
				},
			},
			want: "reports list --mode example-value",
		},
		{
			name: "path query default keeps required param default",
			resources: map[string]spec.Resource{
				"reports": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET",
							Path:   "/reports?mode=summary",
							Params: []spec.Param{
								{Name: "mode", Required: true, Type: "string", Default: "summary"},
							},
						},
						"refresh": {Method: "POST", Path: "/reports/refresh"},
					},
				},
			},
			want: "reports list --mode summary",
		},
		{
			name: "non-dispatch string default still uses synthetic value",
			resources: map[string]spec.Resource{
				"search": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET",
							Path:   "/search",
							Params: []spec.Param{
								{Name: "query", Required: true, Type: "string", Default: "cats"},
							},
						},
						"refresh": {Method: "POST", Path: "/search/refresh"},
					},
				},
			},
			want: "search list --query example-value",
		},
		{
			name: "numeric default still uses synthetic value",
			resources: map[string]spec.Resource{
				"items": {
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET",
							Path:   "/items",
							Params: []spec.Param{
								{Name: "limit", Required: true, Type: "integer", Default: 100},
							},
						},
						"refresh": {Method: "POST", Path: "/items/refresh"},
					},
				},
			},
			want: "items list --limit 50",
		},
		{
			name: "required body field uses public flag name",
			resources: map[string]spec.Resource{
				"stores": {
					Endpoints: map[string]spec.Endpoint{
						"create": {
							Method: "POST",
							Path:   "/stores",
							Body: []spec.Param{
								{Name: "store_code", FlagName: "store-code", Required: true, Type: "string"},
							},
						},
						"refresh": {Method: "POST", Path: "/stores/refresh"},
					},
				},
			},
			want: "stores create --store-code example-value",
		},
		{
			// A JSON-typed string body flag (the command emits a json.Valid
			// guard) must not advertise the scalar "example-value" placeholder,
			// which fails that guard on first run. It gets a minimal valid-JSON
			// value instead, single-quoted for shell safety.
			name: "json body field uses valid-json placeholder",
			resources: map[string]spec.Resource{
				"lists": {
					Endpoints: map[string]spec.Endpoint{
						"add": {
							Method: "POST",
							Path:   "/lists/add.php",
							Body: []spec.Param{
								{Name: "lists", Required: true, Type: "string", Description: "JSON array of list objects"},
							},
						},
						"refresh": {Method: "POST", Path: "/lists/refresh"},
					},
				},
			},
			want: "lists add --lists '[]'",
		},
		{
			name: "json body field with bracketed description uses array placeholder",
			resources: map[string]spec.Resource{
				"items": {
					Endpoints: map[string]spec.Endpoint{
						"bulk": {
							Method: "POST",
							Path:   "/items/bulk",
							Body: []spec.Param{
								{Name: "payload", Required: true, Type: "string", Description: `[{"id":1,"name":"foo"}]`},
							},
						},
					},
				},
			},
			want: "items --payload '[]'",
		},
		{
			name: "promoted single-endpoint resource keeps positionals",
			resources: map[string]spec.Resource{
				"feed": {
					Endpoints: map[string]spec.Endpoint{
						"get-on-this-day": {
							Method: "GET",
							Path:   "/feed/{date}",
							Params: []spec.Param{
								{Name: "date", Positional: true, Default: "2026-04-27"},
							},
						},
					},
				},
			},
			want: "feed 2026-04-27",
		},
		{
			// Issue #1270: snake_case endpoint keys must be kebab-cased so
			// the example matches the actual cobra `Use:` string (which is
			// already kebab) instead of advertising a phantom command path.
			name: "multi-word snake_case endpoint key is kebab-cased",
			resources: map[string]spec.Resource{
				"dns": {
					Endpoints: map[string]spec.Endpoint{
						"get_hosts":            {Method: "GET", Path: "/dns/hosts"},
						"set_email_forwarding": {Method: "POST", Path: "/dns/forwarding"},
					},
				},
			},
			want: "dns get-hosts",
		},
		{
			// Same kebab pass for camelCase endpoint keys. Mirrors the
			// command_endpoint.go.tmpl `Use: {{kebab .EndpointName}}` rule
			// for cobra command names. The preferred-verb scan misses
			// `createSpeech` and `cancelJob`, so the fallback alphabetical
			// pick is `cancelJob` (c < other letters).
			name: "multi-word camelCase endpoint key is kebab-cased",
			resources: map[string]spec.Resource{
				"audio": {
					Endpoints: map[string]spec.Endpoint{
						"createSpeech": {Method: "POST", Path: "/audio/speech"},
						"cancelJob":    {Method: "POST", Path: "/audio/cancel"},
					},
				},
			},
			want: "audio cancel-job",
		},
		{
			// Issue #1853: PascalCase resource keys (sniffed .NET/Java
			// enterprise APIs) must be kebab-cased to match the actual cobra
			// command name. Without this the example advertises a phantom
			// `cli ChangeOrders` path that exits "unknown command".
			name: "PascalCase resource key is kebab-cased (promoted)",
			resources: map[string]spec.Resource{
				"ChangeOrders": {
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/api/ChangeOrders/Grid"},
					},
				},
			},
			want: "change-orders",
		},
		{
			// Same kebab pass when the resource is not promoted (multiple
			// endpoints): both the resource and endpoint segments are kebab.
			name: "PascalCase resource key is kebab-cased (non-promoted)",
			resources: map[string]spec.Resource{
				"PurchaseOrders": {
					Endpoints: map[string]spec.Endpoint{
						"list": {Method: "GET", Path: "/api/PurchaseOrders"},
						"get":  {Method: "GET", Path: "/api/PurchaseOrders/{id}"},
					},
				},
			},
			want: "purchase-orders list",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, firstCommandExample(tc.resources))
		})
	}
}
