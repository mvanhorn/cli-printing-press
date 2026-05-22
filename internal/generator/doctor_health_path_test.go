package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveHealthCheckPath_PrioritizesExplicitOverride(t *testing.T) {
	t.Parallel()

	s := minimalSpec("explicit")
	s.HealthCheckPath = "/api/health"
	s.Auth.VerifyPath = "/me"
	s.Resources["users"] = spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"me": {Method: "GET", Path: "/users/me"},
		},
	}

	assert.Equal(t, "/api/health", deriveHealthCheckPath(s),
		"explicit HealthCheckPath must not be overwritten by fallbacks")
}

func TestDeriveHealthCheckPath_PrefersAuthVerifyPath(t *testing.T) {
	t.Parallel()

	s := minimalSpec("verify-path")
	s.Auth.VerifyPath = "/v1/account"
	s.Resources["users"] = spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"me": {Method: "GET", Path: "/users/me"},
		},
	}

	assert.Equal(t, "/v1/account", deriveHealthCheckPath(s),
		"Auth.VerifyPath should win over a me-shaped heuristic match")
}

func TestDeriveHealthCheckPath_HeuristicMeShapedTails(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want string
	}{
		{"bare me", "/me", "/me"},
		{"me.json", "/me.json", "/me.json"},
		{"users/me", "/users/me", "/users/me"},
		{"users/me.json (Zendesk)", "/api/v2/users/me.json", "/api/v2/users/me.json"},
		{"user (GitHub)", "/user", "/user"},
		{"viewer", "/viewer", "/viewer"},
		{"whoami", "/api/whoami", "/api/whoami"},
		{"self", "/v1/self", "/v1/self"},
		{"account", "/v1/account", "/v1/account"},
		{"users/@me (Discord)", "/api/users/@me", "/api/users/@me"},
		{"current_user", "/api/current_user", "/api/current_user"},
		{"mixed case path", "/Users/Me", "/Users/Me"},
		{"trailing slash", "/users/me/", "/users/me/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &spec.APISpec{
				Name:    "heuristic",
				BaseURL: "https://api.example.com",
				Resources: map[string]spec.Resource{
					"probe": {Endpoints: map[string]spec.Endpoint{
						"probe": {Method: "GET", Path: tc.path},
					}},
				},
			}
			assert.Equal(t, tc.want, deriveHealthCheckPath(s))
		})
	}
}

func TestDeriveHealthCheckPath_SkipsPathsWithPlaceholders(t *testing.T) {
	t.Parallel()

	// `/{tenant}/me` would match the bare `me` tail if the placeholder
	// guard were removed — that makes the guard load-bearing on this test.
	s := &spec.APISpec{
		Name: "placeholder",
		Resources: map[string]spec.Resource{
			"users": {Endpoints: map[string]spec.Endpoint{
				"me": {Method: "GET", Path: "/{tenant}/me"},
			}},
		},
	}
	assert.Equal(t, "", deriveHealthCheckPath(s),
		"paths with {placeholders} cannot be probed without inputs")
}

func TestDeriveHealthCheckPath_RejectsSubstringSegmentMatches(t *testing.T) {
	t.Parallel()

	// `/some_account` ends with the string "account" but not the segment
	// "account"; the boundary anchor `"/"+target` should keep it out.
	cases := []string{"/some_account", "/admin/notme", "/account-management"}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			s := &spec.APISpec{
				Resources: map[string]spec.Resource{
					"r": {Endpoints: map[string]spec.Endpoint{
						"e": {Method: "GET", Path: path},
					}},
				},
			}
			assert.Equal(t, "", deriveHealthCheckPath(s),
				"%s shares only a substring with a tail; must not match", path)
		})
	}
}

func TestDeriveHealthCheckPath_SkipsRequiredQueryParams(t *testing.T) {
	t.Parallel()

	s := &spec.APISpec{
		Name: "required-params",
		Resources: map[string]spec.Resource{
			"users": {Endpoints: map[string]spec.Endpoint{
				"me": {
					Method: "GET",
					Path:   "/users/me",
					Params: []spec.Param{{Name: "fields", Required: true}},
				},
			}},
		},
	}
	assert.Equal(t, "", deriveHealthCheckPath(s),
		"endpoints with required params can't be safely probed unauthenticated")
}

func TestDeriveHealthCheckPath_SkipsNonGet(t *testing.T) {
	t.Parallel()

	s := &spec.APISpec{
		Name: "non-get",
		Resources: map[string]spec.Resource{
			"users": {Endpoints: map[string]spec.Endpoint{
				"create-me": {Method: "POST", Path: "/users/me"},
			}},
		},
	}
	assert.Equal(t, "", deriveHealthCheckPath(s))
}

func TestDeriveHealthCheckPath_WalksSubResources(t *testing.T) {
	t.Parallel()

	s := &spec.APISpec{
		Name: "nested",
		Resources: map[string]spec.Resource{
			"top": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/top"},
				},
				SubResources: map[string]spec.Resource{
					"users": {Endpoints: map[string]spec.Endpoint{
						"me": {Method: "GET", Path: "/users/me"},
					}},
				},
			},
		},
	}
	assert.Equal(t, "/users/me", deriveHealthCheckPath(s))
}

func TestDeriveHealthCheckPath_PrefersSpecificTailOverBare(t *testing.T) {
	t.Parallel()

	// A spec that ships both `/me` and `/users/me` should land on
	// `/users/me` — meShapedPathTails orders the more specific tail first.
	s := &spec.APISpec{
		Name: "both",
		Resources: map[string]spec.Resource{
			"r": {Endpoints: map[string]spec.Endpoint{
				"a": {Method: "GET", Path: "/me"},
				"b": {Method: "GET", Path: "/users/me"},
			}},
		},
	}
	assert.Equal(t, "/users/me", deriveHealthCheckPath(s))
}

func TestDeriveHealthCheckPath_FallsBackToEmpty(t *testing.T) {
	t.Parallel()

	s := &spec.APISpec{
		Name: "nothing-suitable",
		Resources: map[string]spec.Resource{
			"items": {Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/items"},
				"create": {Method: "POST", Path: "/items"},
			}},
		},
	}
	assert.Equal(t, "", deriveHealthCheckPath(s),
		"no me-shaped tail matches → template falls back to `/`")
}

func TestDeriveHealthCheckPath_NilSpec(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", deriveHealthCheckPath(nil))
}

func TestDeriveHealthCheckPath_DeterministicSameTailCollision(t *testing.T) {
	t.Parallel()

	// Two GET endpoints in different resources both match the bare `me`
	// tail. findEndpointMatch iterates sorted resource keys, so `a-resource`
	// wins regardless of map iteration order. Re-run the helper 50 times to
	// catch any non-determinism that slipped past a single happy-path run.
	build := func() *spec.APISpec {
		return &spec.APISpec{
			Name: "collide",
			Resources: map[string]spec.Resource{
				"z-resource": {Endpoints: map[string]spec.Endpoint{
					"me": {Method: "GET", Path: "/z/me"},
				}},
				"a-resource": {Endpoints: map[string]spec.Endpoint{
					"me": {Method: "GET", Path: "/a/me"},
				}},
			},
		}
	}
	for range 50 {
		assert.Equal(t, "/a/me", deriveHealthCheckPath(build()))
	}
}

func TestGenerate_HealthCheckPathDerivationIsIdempotent(t *testing.T) {
	t.Parallel()

	// Generate() mutates g.Spec.HealthCheckPath when empty. A second call
	// on the same spec must re-emit byte-identical output — regen-merge and
	// mcp-sync re-enter Generate() on a spec that already saw the
	// derivation pass.
	apiSpec := minimalSpec("idempotent")
	apiSpec.Auth.VerifyPath = "/v1/account"

	firstDir := filepath.Join(t.TempDir(), "first")
	require.NoError(t, New(apiSpec, firstDir).Generate())
	first, err := os.ReadFile(filepath.Join(firstDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)

	secondDir := filepath.Join(t.TempDir(), "second")
	require.NoError(t, New(apiSpec, secondDir).Generate())
	second, err := os.ReadFile(filepath.Join(secondDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second),
		"second Generate() must emit byte-identical doctor.go")
	assert.Equal(t, "/v1/account", apiSpec.HealthCheckPath,
		"derived value should be visible on spec after Generate()")
}

// TestGeneratedDoctor_DerivesHealthCheckPathFromVerifyPath wires the helper
// through Generate(): a spec with only Auth.VerifyPath set and no explicit
// HealthCheckPath must emit a doctor.go that probes the verify path, not `/`.
func TestGeneratedDoctor_DerivesHealthCheckPathFromVerifyPath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("derive-verify")
	apiSpec.Auth.VerifyPath = "/v1/account"

	outputDir := filepath.Join(t.TempDir(), "derive-verify-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	assert.Contains(t, content, `healthPath := "/v1/account"`,
		"doctor should probe Auth.VerifyPath when HealthCheckPath is unset")
	assert.NotContains(t, content, `reachBody, reachErr := c.Get(cmd.Context(), "/", nil)`,
		"the bare-root fallback branch should not be rendered when a derived path exists")
}

// TestGeneratedDoctor_DerivesHealthCheckPathFromMeEndpoint mirrors the above
// for the heuristic case — no Auth.VerifyPath, but a me-shaped GET endpoint
// in the spec should be picked up.
func TestGeneratedDoctor_DerivesHealthCheckPathFromMeEndpoint(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("derive-me")
	apiSpec.Resources["users"] = spec.Resource{
		Description: "Users",
		Endpoints: map[string]spec.Endpoint{
			"me": {Method: "GET", Path: "/users/me", Description: "Get current user"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "derive-me-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	assert.Contains(t, content, `healthPath := "/users/me"`)
}

// TestGeneratedDoctor_KeepsExplicitHealthCheckPath guards against the helper
// overwriting an explicit override. Pairs with the priority logic in
// deriveHealthCheckPath.
func TestGeneratedDoctor_KeepsExplicitHealthCheckPath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("explicit-override")
	apiSpec.HealthCheckPath = "api/marketStatus"
	apiSpec.Auth.VerifyPath = "/me" // would otherwise win over the heuristic

	outputDir := filepath.Join(t.TempDir(), "explicit-override-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	assert.Contains(t, string(doctorGo), `healthPath := "api/marketStatus"`)
}

func TestDeriveAuthVerifyPath_PrioritizesExplicitOverride(t *testing.T) {
	t.Parallel()

	s := minimalSpec("explicit-verify")
	s.Auth.VerifyPath = "/operator-vetted"
	// Add a me-shaped endpoint that would otherwise win.
	s.Resources["users"] = spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"me": {Method: "GET", Path: "/users/me"},
		},
	}

	assert.Equal(t, "/operator-vetted", deriveAuthVerifyPath(s),
		"explicit Auth.VerifyPath must not be overwritten by the me-shaped heuristic")
}

func TestDeriveAuthVerifyPath_PicksMeShapedEndpoint(t *testing.T) {
	t.Parallel()

	s := minimalSpec("me-fallback")
	s.Resources["users"] = spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"me": {Method: "GET", Path: "/users/me"},
		},
	}

	assert.Equal(t, "/users/me", deriveAuthVerifyPath(s),
		"a me-shaped GET should be discovered when Auth.VerifyPath is unset")
}

func TestDeriveAuthVerifyPath_FallsBackToEmpty(t *testing.T) {
	t.Parallel()

	// minimalSpec's only endpoint is /items — not me-shaped. The function
	// must return "" so the doctor template keeps emitting the existing
	// "present (not verified)" branch rather than fabricating a probe.
	s := minimalSpec("no-candidate")
	assert.Equal(t, "", deriveAuthVerifyPath(s),
		"no me-shaped tail match must surface as empty so the template fallback wins")
}

func TestDeriveAuthVerifyPath_NilSpec(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", deriveAuthVerifyPath(nil))
}

// TestGeneratedDoctor_DerivesAuthVerifyPathFromMeEndpoint wires the helper
// through Generate(): a spec with a me-shaped GET and no explicit
// Auth.VerifyPath must emit a doctor.go that actually probes that endpoint
// for credential validity, not the "present (not verified)" placeholder.
func TestGeneratedDoctor_DerivesAuthVerifyPathFromMeEndpoint(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("derive-verify-me")
	apiSpec.Resources["users"] = spec.Resource{
		Description: "Users",
		Endpoints: map[string]spec.Endpoint{
			"me": {Method: "GET", Path: "/users/me", Description: "Get current user"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "derive-verify-me-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	assert.Contains(t, content, `verifyPath := "/users/me"`,
		"doctor should probe the derived me-shaped path for credential validity")
	assert.Contains(t, content, `c.GetWithHeaders(cmd.Context(), verifyPath`,
		"doctor should issue the authenticated probe through the configured client")
	assert.NotContains(t, content, `"present (not verified — set auth.verify_path in spec for an API acceptance check)"`,
		"the no-verify-path placeholder branch must not be rendered once a path is derived")
	assert.Equal(t, "/users/me", apiSpec.Auth.VerifyPath,
		"derived value should be visible on spec after Generate()")
}

// TestGeneratedDoctor_KeepsExplicitAuthVerifyPath guards the override path:
// an operator-supplied auth.verify_path must survive Generate() even when the
// spec also ships a me-shaped endpoint that the heuristic would otherwise pick.
func TestGeneratedDoctor_KeepsExplicitAuthVerifyPath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("explicit-verify-override")
	apiSpec.Auth.VerifyPath = "/operator-vetted"
	apiSpec.Resources["users"] = spec.Resource{
		Description: "Users",
		Endpoints: map[string]spec.Endpoint{
			"me": {Method: "GET", Path: "/users/me", Description: "Get current user"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "explicit-verify-override-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	assert.Contains(t, content, `verifyPath := "/operator-vetted"`)
	assert.NotContains(t, content, `verifyPath := "/users/me"`)
}

// TestGenerate_AuthVerifyPathDerivationIsIdempotent pins re-entry behavior:
// regen-merge and mcp-sync re-invoke Generate() on a spec that already saw
// the derivation pass; the second call must observe the populated value and
// emit byte-identical output.
func TestGenerate_AuthVerifyPathDerivationIsIdempotent(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("verify-idempotent")
	apiSpec.Resources["users"] = spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"me": {Method: "GET", Path: "/users/me"},
		},
	}

	firstDir := filepath.Join(t.TempDir(), "first")
	require.NoError(t, New(apiSpec, firstDir).Generate())
	first, err := os.ReadFile(filepath.Join(firstDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)

	secondDir := filepath.Join(t.TempDir(), "second")
	require.NoError(t, New(apiSpec, secondDir).Generate())
	second, err := os.ReadFile(filepath.Join(secondDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second),
		"second Generate() must emit byte-identical doctor.go")
	assert.Equal(t, "/users/me", apiSpec.Auth.VerifyPath,
		"derived value should persist on spec across Generate() calls")
}

// TestGeneratedDoctor_NoCandidateFallsBackToRoot keeps the negative case
// stable: a spec with nothing me-shaped renders the `/`-probe branch the
// pre-derivation template has always emitted.
func TestGeneratedDoctor_NoCandidateFallsBackToRoot(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "fallback",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/fallback-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "fallback-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	assert.Contains(t, content, `reachBody, reachErr := c.Get(cmd.Context(), "/", nil)`,
		"specs with no derivable probe path should keep the bare-root fallback")
	assert.NotContains(t, content, `healthPath := "`,
		"no healthPath variable should be declared when the spec has nothing to derive")
}
