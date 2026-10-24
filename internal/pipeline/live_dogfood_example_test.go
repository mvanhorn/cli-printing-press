package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	apispec "github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const examplelessLiveDogfoodSpec = `name: exampleless
version: "0.1.0"
base_url: "https://api.example.com"
auth:
  type: none
config:
  format: toml
  path: "~/.config/exampleless/config.toml"
resources:
  items:
    description: Items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
        response:
          type: object
      get:
        method: GET
        path: /items/{id}
        description: Get an item
        params:
          - name: id
            type: string
            required: true
            positional: true
        response:
          type: object
  comments:
    description: Comments
    endpoints:
      create:
        method: POST
        path: /comments
        description: Create a comment
        body:
          - name: message
            type: string
            required: true
        response:
          type: object
  videos:
    description: Videos
    endpoints:
      get:
        method: GET
        path: /videos/{video_id}
        description: Get a video
        params:
          - name: video_id
            type: string
            required: true
            positional: true
        response:
          type: object
`

const derivableMissingExampleSpec = `name: exampleless
version: "0.1.0"
base_url: "https://api.example.com"
auth:
  type: none
config:
  format: toml
  path: "~/.config/exampleless/config.toml"
resources:
  items:
    description: Items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
        response:
          type: object
      search:
        method: GET
        path: /items/search
        description: Search items
        params:
          - name: q
            type: string
            required: true
            example: cats
        response:
          type: object
  transfers:
    description: Transfers
    endpoints:
      create:
        method: POST
        path: /transfers
        description: Create a transfer
        body:
          - name: memo
            type: string
            required: true
            example: "invoice 41"
        response:
          type: object
  videos:
    description: Videos
    endpoints:
      get:
        method: GET
        path: /videos/{video_id}
        description: Get a video
        example: "  fixture-pp-cli videos get real-id"
        params:
          - name: video_id
            type: string
            required: true
            positional: true
        response:
          type: object
`

func TestExamplelessSpecPredicates(t *testing.T) {
	t.Parallel()

	parsed, err := apispec.ParseBytes([]byte(examplelessLiveDogfoodSpec))
	require.NoError(t, err)
	endpoints := liveDogfoodSpecEndpoints(parsed)

	assert.True(t, generator.ExampleOmittedForUnderivableInputs(findLiveDogfoodSpecEndpoint(t, endpoints, "items.get").endpoint))
	assert.True(t, generator.ExampleOmittedForUnderivableInputs(findLiveDogfoodSpecEndpoint(t, endpoints, "comments.create").endpoint))
	assert.True(t, generator.ExampleOmittedForUnderivableInputs(findLiveDogfoodSpecEndpoint(t, endpoints, "videos.get").endpoint))
	assert.False(t, generator.ExampleOmittedForUnderivableInputs(findLiveDogfoodSpecEndpoint(t, endpoints, "items.list").endpoint))

	derivable, err := apispec.ParseBytes([]byte(derivableMissingExampleSpec))
	require.NoError(t, err)
	derivableEndpoints := liveDogfoodSpecEndpoints(derivable)
	assert.False(t, generator.ExampleOmittedForUnderivableInputs(findLiveDogfoodSpecEndpoint(t, derivableEndpoints, "items.search").endpoint))
	assert.False(t, generator.ExampleOmittedForUnderivableInputs(findLiveDogfoodSpecEndpoint(t, derivableEndpoints, "transfers.create").endpoint))
	assert.False(t, generator.ExampleOmittedForUnderivableInputs(findLiveDogfoodSpecEndpoint(t, derivableEndpoints, "videos.get").endpoint))
}

func TestLiveDogfoodOmittedExampleSkipReason(t *testing.T) {
	t.Parallel()

	ambiguous := `name: ambiguous
version: "0.1.0"
base_url: "https://api.example.com"
auth:
  type: none
config:
  format: toml
  path: "~/.config/ambiguous/config.toml"
resources:
  posts:
    description: Posts
    base_url: https://posts.example.com
    sub_resources:
      comments:
        description: Post comments
        endpoints:
          create:
            method: POST
            path: /posts/{id}/comments
            description: Create a post comment
            params:
              - name: id
                type: string
                required: true
                positional: true
            response:
              type: object
  pages:
    description: Pages
    base_url: https://pages.example.com
    sub_resources:
      comments:
        description: Page comments
        endpoints:
          create:
            method: POST
            path: /pages/comments
            description: Create a page comment
            params:
              - name: tone
                type: string
                required: true
                example: calm
            response:
              type: object
`
	parsed, err := apispec.ParseBytes([]byte(ambiguous))
	require.NoError(t, err)
	endpoints := liveDogfoodSpecEndpoints(parsed)

	var paths []string
	for _, endpoint := range endpoints {
		if endpoint.annotation == "comments.create" {
			paths = append(paths, endpoint.path)
		}
		assert.NotContains(t, endpoint.annotation, "posts.comments")
		assert.NotContains(t, endpoint.annotation, "pages.comments")
	}
	assert.ElementsMatch(t, []string{
		"https://posts.example.com/posts/{id}/comments",
		"https://pages.example.com/pages/comments",
	}, paths)

	postComment := liveDogfoodCommand{Annotations: map[string]string{
		endpointAnnotation:       "comments.create",
		endpointMethodAnnotation: "POST",
		endpointPathAnnotation:   "https://posts.example.com/posts/{id}/comments",
	}}
	pageComment := liveDogfoodCommand{Annotations: map[string]string{
		endpointAnnotation:       "comments.create",
		endpointMethodAnnotation: "POST",
		endpointPathAnnotation:   "https://pages.example.com/pages/comments",
	}}
	unscoped := liveDogfoodCommand{Annotations: map[string]string{endpointAnnotation: "comments.create"}}
	wrongPath := liveDogfoodCommand{Annotations: map[string]string{
		endpointAnnotation:       "comments.create",
		endpointMethodAnnotation: "POST",
		endpointPathAnnotation:   "/elsewhere",
	}}

	assert.Equal(t, reasonExampleOmittedUnderivableInputs, liveDogfoodOmittedExampleSkipReason(postComment, endpoints))
	assert.Empty(t, liveDogfoodOmittedExampleSkipReason(pageComment, endpoints))
	assert.Empty(t, liveDogfoodOmittedExampleSkipReason(unscoped, endpoints))
	assert.Empty(t, liveDogfoodOmittedExampleSkipReason(wrongPath, endpoints))
	assert.Empty(t, liveDogfoodOmittedExampleSkipReason(liveDogfoodCommand{}, endpoints))
	assert.Empty(t, liveDogfoodOmittedExampleSkipReason(postComment, nil))
	assert.Empty(t, liveDogfoodOmittedExampleSkipReason(liveDogfoodCommand{Annotations: map[string]string{endpointAnnotation: "missing.get"}}, endpoints))

	assert.False(t, liveDogfoodCommandHasDeclaredFixture(liveDogfoodCommand{}))
	assert.False(t, liveDogfoodCommandHasDeclaredFixture(liveDogfoodCommand{Annotations: map[string]string{happyArgsAnnotation: "  "}}))
	assert.True(t, liveDogfoodCommandHasDeclaredFixture(liveDogfoodCommand{Annotations: map[string]string{happyArgsAnnotation: "video_id=real-id"}}))
	assert.True(t, liveDogfoodCommandHasDeclaredFixture(liveDogfoodCommand{Annotations: map[string]string{happyStdinAnnotation: `{"message":"hi"}`}}))
}

func TestLiveDogfoodParsedSpecSkipsUnreadableSpecs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.yaml"), []byte("this is not a spec\n"), 0o644))
	assert.Nil(t, loadLiveDogfoodParsedSpec(dir))
	assert.Nil(t, loadLiveDogfoodBodyFixtures(dir))
	assert.Nil(t, liveDogfoodSpecEndpoints(nil))

	empty := t.TempDir()
	assert.Nil(t, loadLiveDogfoodParsedSpec(empty))
}

func TestRunLiveDogfoodSkipsHelpWhenRequiredInputsAreNotDerivable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.yaml"), []byte(examplelessLiveDogfoodSpec), 0o644))
	script := `#!/bin/sh
set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"items","subcommands":[
      {"name":"list","annotations":{"pp:endpoint":"items.list","pp:method":"GET","pp:path":"/items","mcp:read-only":"true"}},
      {"name":"get","annotations":{"pp:endpoint":"items.get","pp:method":"GET","pp:path":"/items/{id}","mcp:read-only":"true"}}
    ]},
    {"name":"comments","subcommands":[
      {"name":"create","annotations":{"pp:endpoint":"comments.create","pp:method":"POST","pp:path":"/comments"}}
    ]},
    {"name":"videos","subcommands":[
      {"name":"get","annotations":{"pp:endpoint":"videos.get","pp:method":"GET","pp:path":"/videos/{video_id}","pp:happy-args":"video_id=real-id","mcp:read-only":"true"}}
    ]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "items" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List items.

Usage:
  fixture-pp-cli items list [flags]

Examples:
  fixture-pp-cli items list

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "items" ] && [ "$2" = "list" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"results":[{"id":"1"}]}'
    exit 0
  fi
  echo items
  exit 0
fi

if [ "$1" = "items" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get an item.

Usage:
  fixture-pp-cli items get <id> [flags]

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "comments" ] && [ "$2" = "create" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Create a comment.

Usage:
  fixture-pp-cli comments create [flags]

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "videos" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a video.

Usage:
  fixture-pp-cli videos get <video_id> [flags]

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "videos" ] && [ "$2" = "get" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo invalid >&2
    exit 2
  fi
  if [ "${3:-}" = "--json" ] || [ "${4:-}" = "--json" ]; then
    echo '{"id":"real-id"}'
    exit 0
  fi
  echo video
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, binaryName), []byte(script), 0o755))

	markerPath := filepath.Join(t.TempDir(), Phase5AcceptanceFilename)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              dir,
		BinaryName:          binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		WriteAcceptancePath: markerPath,
	})
	require.NoError(t, err)
	require.Equal(t, "PASS", report.Verdict, report.Tests)
	assert.Equal(t, 0, report.Failed)

	listHelp := findResultByCommandKind(report, "items list", LiveDogfoodTestHelp)
	require.NotNil(t, listHelp)
	assert.Equal(t, LiveDogfoodStatusPass, listHelp.Status, listHelp.Reason)

	for _, command := range []string{"items get", "comments create"} {
		help := findResultByCommandKind(report, command, LiveDogfoodTestHelp)
		require.NotNil(t, help, command)
		assert.Equal(t, LiveDogfoodStatusSkip, help.Status, command)
		assert.Equal(t, reasonExampleOmittedUnderivableInputs, help.Reason)
		for _, kind := range []LiveDogfoodTestKind{LiveDogfoodTestHappy, LiveDogfoodTestJSON, LiveDogfoodTestError} {
			result := findResultByCommandKind(report, command, kind)
			require.NotNil(t, result, "%s %s", command, kind)
			assert.Equal(t, LiveDogfoodStatusSkip, result.Status, "%s %s: %s", command, kind, result.Reason)
			assert.Equal(t, reasonExampleOmittedUnderivableInputs, result.Reason)
		}
	}

	videoHelp := findResultByCommandKind(report, "videos get", LiveDogfoodTestHelp)
	require.NotNil(t, videoHelp)
	assert.Equal(t, LiveDogfoodStatusSkip, videoHelp.Status, videoHelp.Reason)
	assert.Equal(t, reasonExampleOmittedUnderivableInputs, videoHelp.Reason)
	videoHappy := findResultByCommandKind(report, "videos get", LiveDogfoodTestHappy)
	require.NotNil(t, videoHappy)
	assert.Equal(t, LiveDogfoodStatusPass, videoHappy.Status, videoHappy.Reason)
	assert.Contains(t, videoHappy.Args, "real-id")

	for _, result := range report.Tests {
		assert.NotEqual(t, LiveDogfoodStatusFail, result.Status, "%s %s: %s", result.Command, result.Kind, result.Reason)
	}

	data, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, "pass", marker.Status)
	assert.Equal(t, 0, marker.TestsFailed)
	validation := ValidatePhase5Gate(filepath.Dir(markerPath), CLIManifest{APIName: marker.APIName, RunID: marker.RunID, AuthType: "none"}, dir)
	assert.True(t, validation.Passed, validation.Detail)
}

func TestRunLiveDogfoodFailsHelpWhenDerivableExampleIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.yaml"), []byte(derivableMissingExampleSpec), 0o644))
	script := `#!/bin/sh
set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"items","subcommands":[
      {"name":"list","annotations":{"pp:endpoint":"items.list","pp:method":"GET","pp:path":"/items","mcp:read-only":"true"}},
      {"name":"search","annotations":{"pp:endpoint":"items.search","pp:method":"GET","pp:path":"/items/search","mcp:read-only":"true"}}
    ]},
    {"name":"transfers","subcommands":[
      {"name":"create","annotations":{"pp:endpoint":"transfers.create","pp:method":"POST","pp:path":"/transfers"}}
    ]},
    {"name":"videos","subcommands":[
      {"name":"get","annotations":{"pp:endpoint":"videos.get","pp:method":"GET","pp:path":"/videos/{video_id}","mcp:read-only":"true"}}
    ]},
    {"name":"digest"}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "items" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List items.

Usage:
  fixture-pp-cli items list [flags]

Examples:
  fixture-pp-cli items list

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "items" ] && [ "$2" = "list" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"results":[{"id":"1"}]}'
    exit 0
  fi
  echo items
  exit 0
fi

if [ "$1" = "items" ] && [ "$2" = "search" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Search items.

Usage:
  fixture-pp-cli items search [flags]

Flags:
      --json    Output JSON
      --q       Query
HELP
  exit 0
fi

if [ "$1" = "transfers" ] && [ "$2" = "create" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Create a transfer.

Usage:
  fixture-pp-cli transfers create [flags]

Flags:
      --json    Output JSON
      --memo    Memo
HELP
  exit 0
fi

if [ "$1" = "videos" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a video.

Usage:
  fixture-pp-cli videos get <video_id> [flags]

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "digest" ] && [ "${2:-}" = "--help" ]; then
  cat <<'HELP'
Digest the catalog.

Usage:
  fixture-pp-cli digest [flags]

Flags:
      --json    Output JSON
HELP
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, binaryName), []byte(script), 0o755))

	markerPath := filepath.Join(t.TempDir(), Phase5AcceptanceFilename)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              dir,
		BinaryName:          binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		WriteAcceptancePath: markerPath,
	})
	require.NoError(t, err)
	require.Equal(t, "FAIL", report.Verdict, report.Tests)
	assert.Greater(t, report.Failed, 0)

	listHelp := findResultByCommandKind(report, "items list", LiveDogfoodTestHelp)
	require.NotNil(t, listHelp)
	assert.Equal(t, LiveDogfoodStatusPass, listHelp.Status, listHelp.Reason)

	for _, command := range []string{"items search", "transfers create", "videos get", "digest"} {
		help := findResultByCommandKind(report, command, LiveDogfoodTestHelp)
		require.NotNil(t, help, command)
		assert.Equal(t, LiveDogfoodStatusFail, help.Status, command)
		assert.Equal(t, "missing Examples section", help.Reason)
	}

	data, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, "fail", marker.Status)
	assert.NotEqual(t, 0, marker.TestsFailed)
	validation := ValidatePhase5Gate(filepath.Dir(markerPath), CLIManifest{APIName: marker.APIName, RunID: marker.RunID, AuthType: "none"}, dir)
	assert.False(t, validation.Passed, validation.Detail)
}

func findLiveDogfoodSpecEndpoint(t *testing.T, endpoints []liveDogfoodSpecEndpoint, annotation string) liveDogfoodSpecEndpoint {
	t.Helper()
	for _, endpoint := range endpoints {
		if endpoint.annotation == annotation {
			return endpoint
		}
	}
	t.Fatalf("endpoint %s not indexed", annotation)
	return liveDogfoodSpecEndpoint{}
}
