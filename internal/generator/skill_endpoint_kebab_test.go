// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSkillAndReadmeKebabCaseMultiWordEndpoints covers issue #1270. The
// generator emits the actual cobra command name via `{{kebab .EndpointName}}`,
// so snake_case or camelCase spec keys ship as kebab-case subcommands. The
// SKILL.md / README.md "Command Reference" sections must show the same kebab
// form -- otherwise verify-skill rejects the example as an unknown command and
// agents follow a phantom path.
func TestSkillAndReadmeKebabCaseMultiWordEndpoints(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("namecheck")
	// Multi-endpoint resource so promotion to a leaf does not fire and the
	// per-endpoint emission path is exercised.
	apiSpec.Resources["dns"] = spec.Resource{
		Description: "Manage DNS records",
		Endpoints: map[string]spec.Endpoint{
			"get_hosts":            {Method: "GET", Path: "/dns/hosts", Description: "Get DNS host records"},
			"set_email_forwarding": {Method: "POST", Path: "/dns/forwarding", Description: "Configure email forwarding"},
		},
	}
	// camelCase shape -- mirrors what an OpenAPI spec with operationId
	// "getEmailForwarding" lands as.
	apiSpec.Resources["audio"] = spec.Resource{
		Description: "Audio operations",
		Endpoints: map[string]spec.Endpoint{
			"createSpeech": {Method: "POST", Path: "/audio/speech", Description: "Synthesize speech"},
			"cancelJob":    {Method: "POST", Path: "/audio/cancel", Description: "Cancel a job"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "namecheck-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	for _, file := range []string{"SKILL.md", "README.md"} {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(outputDir, file))
			require.NoError(t, err)
			content := string(body)

			assert.Contains(t, content, "namecheck-pp-cli dns get-hosts",
				"snake_case spec key get_hosts must render as kebab subcommand get-hosts")
			assert.Contains(t, content, "namecheck-pp-cli dns set-email-forwarding",
				"snake_case spec key set_email_forwarding must render as kebab set-email-forwarding")
			assert.Contains(t, content, "namecheck-pp-cli audio create-speech",
				"camelCase spec key createSpeech must render as kebab create-speech")
			assert.Contains(t, content, "namecheck-pp-cli audio cancel-job",
				"camelCase spec key cancelJob must render as kebab cancel-job")

			assert.NotContains(t, content, "namecheck-pp-cli dns get_hosts",
				"snake_case form must not leak into user-facing docs; cobra command is get-hosts")
			assert.NotContains(t, content, "namecheck-pp-cli dns set_email_forwarding",
				"snake_case form must not leak into user-facing docs; cobra command is set-email-forwarding")
			assert.NotContains(t, content, "namecheck-pp-cli audio createSpeech",
				"camelCase form must not leak into user-facing docs; cobra command is create-speech")
			assert.NotContains(t, content, "namecheck-pp-cli audio cancelJob",
				"camelCase form must not leak into user-facing docs; cobra command is cancel-job")
		})
	}
}

// TestSkillAndReadmeSingleWordEndpointsUnchanged guards the negative case in
// issue #1270's acceptance: single-word endpoint keys (list, create, get,
// check) must pass through the kebab helper unchanged so existing CLIs do not
// drift on regen.
func TestSkillAndReadmeSingleWordEndpointsUnchanged(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("singleword")
	apiSpec.Resources["widgets"] = spec.Resource{
		Description: "Manage widgets",
		Endpoints: map[string]spec.Endpoint{
			"create": {Method: "POST", Path: "/widgets", Description: "Create a widget"},
			"list":   {Method: "GET", Path: "/widgets", Description: "List widgets"},
			"get":    {Method: "GET", Path: "/widgets/{id}", Description: "Get a widget"},
			"check":  {Method: "POST", Path: "/widgets/check", Description: "Run a check"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "singleword-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	for _, file := range []string{"SKILL.md", "README.md"} {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(outputDir, file))
			require.NoError(t, err)
			content := string(body)

			for _, ep := range []string{"create", "list", "get", "check"} {
				assert.Contains(t, content, "singleword-pp-cli widgets "+ep,
					"single-word endpoint %q should be unchanged in docs", ep)
			}
		})
	}
}
