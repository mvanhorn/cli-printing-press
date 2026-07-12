package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedDoctor_EmitsGraphQLProbeWhenVerifyQuerySet wires the new
// Auth.VerifyQuery spec field through Generate(): a GraphQL CLI that opts
// in by setting verify_query must emit a doctor.go that POSTs the declared
// query and parses the response envelope for top-level errors.
func TestGeneratedDoctor_EmitsGraphQLProbeWhenVerifyQuerySet(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-graphql-probe")
	apiSpec.Auth.VerifyQuery = "{ viewer { id } }"

	outputDir := filepath.Join(t.TempDir(), "doctor-graphql-probe-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	assert.Contains(t, content, `verifyBody := map[string]string{"query": "{ viewer { id } }"}`,
		"doctor should serialize the spec-declared verify_query into the request body")
	assert.Contains(t, content, `c.PostQueryWithParamsAndHeaders(cmd.Context(), "",`,
		"doctor should route the GraphQL probe through PostQueryWithParamsAndHeaders so verify-mode does not short-circuit reads")
	assert.Contains(t, content, `Errors []json.RawMessage `+"`json:\"errors\"`",
		"doctor should parse the response for a top-level errors array")
	assert.Contains(t, content, `"rejected — GraphQL response contained top-level errors"`,
		"doctor must distinguish a 2xx-with-errors GraphQL response from a true valid token")
	assert.NotContains(t, content, `"present, not verified.`,
		"the unverified-fallback branch must not be rendered when verify_query is set")

	// The GraphQL probe must split 401 from 403 the same way the REST probe
	// does: 403 means a valid-but-scope-limited token, so telling the operator
	// to "check your credentials" (replace the token) would be wrong guidance.
	assert.Contains(t, content, `case gqlAPIErr.StatusCode == 401:`,
		"GraphQL probe must keep a dedicated 401 branch so an invalid token reads as invalid")
	assert.Contains(t, content, `case gqlAPIErr.StatusCode == 403:`,
		"GraphQL probe must split 403 so a scope-limited token is not misreported as invalid")
	assert.Contains(t, content, `scope-limited (HTTP %d) — credentials are valid but lack permission for this endpoint.`,
		"GraphQL probe's 403 message must point at scope, not the credential value")
	assert.NotContains(t, content, `gqlAPIErr.StatusCode == 401 || gqlAPIErr.StatusCode == 403`,
		"GraphQL probe must not collapse 401 and 403 into a single invalid branch")
}

// TestGeneratedDoctor_PreferVerifyPathOverVerifyQuery pins the precedence
// order: when a spec author declares both, the REST probe wins because it's
// cheaper than a GraphQL POST. The verify_query branch must not emit.
func TestGeneratedDoctor_PreferVerifyPathOverVerifyQuery(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-both-probes")
	apiSpec.Auth.VerifyPath = "/v1/account"
	apiSpec.Auth.VerifyQuery = "{ viewer { id } }"

	outputDir := filepath.Join(t.TempDir(), "doctor-both-probes-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	assert.Contains(t, content, `verifyPath := "/v1/account"`,
		"VerifyPath should win when both probes are declared")
	assert.NotContains(t, content, `verifyBody := map[string]string{"query":`,
		"the GraphQL probe must not emit when a REST verify_path is declared")
}

// TestGeneratedDoctor_UnverifiedMessageSuggestsReadCommand checks the
// no-probe path: when neither VerifyPath nor VerifyQuery is set, the doctor
// must suggest a runtime-discovered read command instead of nagging the
// user to edit a spec they don't own.
func TestGeneratedDoctor_UnverifiedMessageSuggestsReadCommand(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-unverified-message")

	outputDir := filepath.Join(t.TempDir(), "doctor-unverified-message-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	assert.Contains(t, content, `func suggestReadCommand(root *cobra.Command) string`,
		"doctor template must emit the runtime helper that picks a read command")
	assert.Contains(t, content, `suggestion := suggestReadCommand(cmd.Root())`,
		"the unverified branch must call the helper at doctor-time")
	assert.Contains(t, content, `"present, not verified. Run `,
		"the unverified message must use the new INFO copy")
	assert.NotContains(t, content, `"present (not verified — set auth.verify_path in spec for an API acceptance check)"`,
		"the old WARN copy that scolded the user for spec settings must be gone")

	// minimalSpec ships an items.list endpoint; the helper plus the renderer
	// switch make "not verified" render as yellow INFO, not WARN.
	assert.Contains(t, content, `case strings.Contains(s, "not verified"):`,
		"the renderer must downgrade the unverified-state to INFO; without the case it falls through to the WARN catch-all")
	assert.Contains(t, content, `indicator = yellow("INFO")`,
		"the unverified case must paint yellow INFO, not red FAIL or yellow WARN")
}

// TestGeneratedDoctor_SuggestReadCommandHelperGatesOnEndpointAndArgs guards
// the shape of the runtime helper. It MUST:
//
//   - Only suggest commands carrying the pp:endpoint annotation, so the
//     suggestion actually exercises the token rather than reading a local
//     file (`feedback list`, `profile list`). Without this gate the doctor
//     could recreate the false-confidence failure mode that the new copy
//     was designed to prevent.
//   - Reject Use strings containing `<` or `[` (the positional-arg markers
//     emitted for `get <id>`-style endpoint commands), because their runtime
//     body prints help when args are empty, so the suggestion would not
//     actually call the API.
//   - Restrict to list/get verbs.
//   - Probe the Args validator with [] as a final defense.
func TestGeneratedDoctor_SuggestReadCommandHelperGatesOnEndpointAndArgs(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-suggest-helper")

	outputDir := filepath.Join(t.TempDir(), "doctor-suggest-helper-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorGo)

	assert.Contains(t, content, `if cmd.Annotations["pp:endpoint"] == ""`,
		"helper must reject framework commands without a pp:endpoint annotation")
	assert.Contains(t, content, `strings.ContainsAny(cmd.Use, "<[")`,
		"helper must reject endpoint commands whose Use advertises positional path params")
	assert.Contains(t, content, `verb := strings.ToLower(strings.SplitN(cmd.Use, " ", 2)[0])`,
		"helper must extract the leading verb from Use, not the full string")
	assert.Contains(t, content, `if verb != "list" && verb != "get"`,
		"helper must restrict to list/get verbs only")
	assert.Contains(t, content, `cmd.Args(cmd, []string{}) == nil`,
		"helper must probe the Args validator with an empty arg list as a final defense")

	// Hidden-parent traversal: printed CLIs mark raw resource parents Hidden
	// to keep --help curated, but their endpoint leaves stay runnable. The
	// walk MUST recurse into hidden parents (otherwise the suggestion is ""
	// in nearly every CLI), while isSuggestableReadLeaf MUST still reject a
	// leaf that is itself hidden.
	assert.Contains(t, content, `if cmd == nil || cmd.Hidden || cmd.HasSubCommands() || !cmd.Runnable()`,
		"isSuggestableReadLeaf must reject a leaf that is itself hidden")
	assert.NotContains(t, content, `for _, child := range cmd.Commands() {
			if child.Hidden {
				continue
			}`,
		"the walk must recurse through hidden resource parents, not skip them — skipping makes the suggestion empty in nearly every CLI")
}

// TestGeneratedDoctor_SuggestHelperOnlyEmittedWhenNeeded pins the template
// gate that wraps the helper functions: when a spec declares any probe
// (VerifyPath or VerifyQuery), the helpers are dead code in the printed
// CLI and should not emit at all. The opposite case (no probe declared)
// must still emit them, because the unverified-state branch calls into
// suggestReadCommand at doctor-time.
func TestGeneratedDoctor_SuggestHelperOnlyEmittedWhenNeeded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		configure func(*minimalDoctorSpec)
		wantEmit  bool
	}{
		{
			name:      "no probe — helpers required by the unverified branch",
			configure: func(s *minimalDoctorSpec) {},
			wantEmit:  true,
		},
		{
			name:      "verify_path set — helpers are dead code",
			configure: func(s *minimalDoctorSpec) { s.VerifyPath = "/v1/account" },
			wantEmit:  false,
		},
		{
			name:      "verify_query set — helpers are dead code",
			configure: func(s *minimalDoctorSpec) { s.VerifyQuery = "{ viewer { id } }" },
			wantEmit:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			apiSpec := minimalSpec("doctor-helper-gate")
			cfg := &minimalDoctorSpec{}
			tc.configure(cfg)
			apiSpec.Auth.VerifyPath = cfg.VerifyPath
			apiSpec.Auth.VerifyQuery = cfg.VerifyQuery

			outputDir := filepath.Join(t.TempDir(), "doctor-helper-gate-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
			require.NoError(t, err)
			content := string(doctorGo)

			if tc.wantEmit {
				assert.Contains(t, content, `func suggestReadCommand(root *cobra.Command) string`,
					"helper must emit when no probe is declared (unverified branch calls it)")
			} else {
				assert.NotContains(t, content, `func suggestReadCommand(root *cobra.Command) string`,
					"helper must not emit when a probe is declared — dead code in the printed CLI")
			}
		})
	}
}

type minimalDoctorSpec struct {
	VerifyPath  string
	VerifyQuery string
}
