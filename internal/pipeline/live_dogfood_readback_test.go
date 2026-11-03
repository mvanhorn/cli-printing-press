package pipeline

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const writeReadbackUpdateHelp = `Update an issue.

Usage:
  fixture-pp-cli issues update <id> [flags]

Examples:
  fixture-pp-cli issues update 1 --subject=renamed

Flags:
  -s, --subject string   New subject
      --closed           Close the issue
      --note-id string   Note id
      --json             Output JSON
`

const writeReadbackGetHelp = `Get an issue.

Usage:
  fixture-pp-cli issues get <id> [flags]

Examples:
  fixture-pp-cli issues get 1

Flags:
      --json   Output JSON
`

const writeReadbackGetByFlagHelp = `Get an issue.

Usage:
  fixture-pp-cli issues get [flags]

Examples:
  fixture-pp-cli issues get --id=1

Flags:
      --id string   Issue id
      --json        Output JSON
`

func TestLiveDogfoodWriteReadbackKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  liveDogfoodCommand
		want writeReadbackKind
	}{
		{name: "put is update", cmd: liveDogfoodCommand{Path: []string{"issues", "save"}, Annotations: map[string]string{"pp:method": "PUT"}}, want: writeReadbackKindUpdate},
		{name: "patch leaf is update", cmd: liveDogfoodCommand{Path: []string{"issues", "patch"}, Annotations: map[string]string{"pp:method": "POST"}}, want: writeReadbackKindUpdate},
		{name: "post create", cmd: liveDogfoodCommand{Path: []string{"issues", "create"}, Annotations: map[string]string{"pp:method": "POST"}}, want: writeReadbackKindCreate},
		{name: "unannotated create leaf", cmd: liveDogfoodCommand{Path: []string{"issues", "create"}}, want: writeReadbackKindCreate},
		{name: "unannotated update leaf", cmd: liveDogfoodCommand{Path: []string{"issues", "update"}}, want: writeReadbackKindUpdate},
		{name: "post render is not a resource write", cmd: liveDogfoodCommand{Path: []string{"render"}, Annotations: map[string]string{"pp:method": "POST"}}},
		{name: "post inspect is not a resource write", cmd: liveDogfoodCommand{Path: []string{"widgets", "inspect"}, Annotations: map[string]string{"pp:method": "POST"}}},
		{name: "delete", cmd: liveDogfoodCommand{Path: []string{"issues", "delete"}, Annotations: map[string]string{"pp:method": "DELETE"}}},
		{name: "head", cmd: liveDogfoodCommand{Path: []string{"issues", "update"}, Annotations: map[string]string{"pp:method": "HEAD"}}},
		{name: "get method", cmd: liveDogfoodCommand{Path: []string{"issues", "update"}, Annotations: map[string]string{"pp:method": "GET"}}},
		{name: "post send is not a resource write", cmd: liveDogfoodCommand{Path: []string{"issues", "send"}, Annotations: map[string]string{"pp:method": "POST"}}},
		{name: "add leaf is create", cmd: liveDogfoodCommand{Path: []string{"issues", "add"}}, want: writeReadbackKindCreate},
		{name: "read only", cmd: liveDogfoodCommand{Path: []string{"issues", "update"}, Annotations: map[string]string{"pp:method": "PUT", "mcp:read-only": "true"}}},
		{name: "local write", cmd: liveDogfoodCommand{Path: []string{"issues", "update"}, Annotations: map[string]string{"pp:method": "PUT", "mcp:local-write": "true"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, liveDogfoodWriteReadbackKind(tt.cmd))
		})
	}
}

func TestLiveDogfoodWriteReadbackApplies(t *testing.T) {
	t.Parallel()

	update := liveDogfoodCommand{Path: []string{"issues", "update"}, Annotations: map[string]string{"pp:method": "PUT"}}
	assert.True(t, liveDogfoodWriteReadbackApplies(update, []string{"issues", "update", "1", "--subject=renamed"}, false))
	assert.False(t, liveDogfoodWriteReadbackApplies(update, []string{"issues", "update", "1", "--dry-run"}, false))
	assert.False(t, liveDogfoodWriteReadbackApplies(update, []string{"issues", "update", "1", "--dry-run=true"}, false))
	assert.True(t, liveDogfoodWriteReadbackApplies(update, []string{"issues", "update", "1", "--dry-run=false"}, false))
	assert.False(t, liveDogfoodWriteReadbackApplies(update, []string{"issues", "update", "1"}, true), "preview runs do not persist")
	render := liveDogfoodCommand{Path: []string{"render"}, Annotations: map[string]string{"pp:method": "POST"}}
	assert.False(t, liveDogfoodWriteReadbackApplies(render, []string{"render"}, false))
}

func TestFindWriteReadbackSibling(t *testing.T) {
	t.Parallel()

	update := liveDogfoodCommand{Path: []string{"issues", "update"}, Annotations: map[string]string{"pp:method": "PUT"}}
	get := liveDogfoodCommand{Path: []string{"issues", "get"}, Annotations: map[string]string{"pp:method": "GET"}}
	show := liveDogfoodCommand{Path: []string{"issues", "show"}, Annotations: map[string]string{"pp:method": "GET"}}
	list := liveDogfoodCommand{Path: []string{"issues", "list"}, Annotations: map[string]string{"pp:method": "GET"}}
	postGet := liveDogfoodCommand{Path: []string{"issues", "get"}, Annotations: map[string]string{"pp:method": "POST"}}
	getAll := liveDogfoodCommand{Path: []string{"issues", "get-all"}, Annotations: map[string]string{"pp:method": "GET"}}
	parent := liveDogfoodCommand{Path: []string{"issues", "view"}, Annotations: map[string]string{"pp:parent-group": "true"}}
	zzz := liveDogfoodCommand{Path: []string{"issues", "zzz-get"}, Annotations: map[string]string{"pp:method": "GET"}}
	aaa := liveDogfoodCommand{Path: []string{"issues", "aaa-get"}, Annotations: map[string]string{"pp:method": "GET"}}

	got := findWriteReadbackSibling(update, []liveDogfoodCommand{list, show, get, update})
	require.NotNil(t, got)
	assert.Equal(t, []string{"issues", "get"}, got.Path)

	assert.Nil(t, findWriteReadbackSibling(update, []liveDogfoodCommand{list, update}))
	assert.Nil(t, findWriteReadbackSibling(update, []liveDogfoodCommand{postGet, getAll, parent}))

	tied := findWriteReadbackSibling(update, []liveDogfoodCommand{zzz, aaa})
	require.NotNil(t, tied)
	assert.Equal(t, []string{"issues", "aaa-get"}, tied.Path)

	readOnlyPost := liveDogfoodCommand{Path: []string{"issues", "get"}, Annotations: map[string]string{"pp:method": "POST", "mcp:read-only": "true"}}
	got = findWriteReadbackSibling(update, []liveDogfoodCommand{readOnlyPost})
	require.NotNil(t, got)
	assert.Equal(t, []string{"issues", "get"}, got.Path)

	unannotated := liveDogfoodCommand{Path: []string{"issues", "show"}}
	got = findWriteReadbackSibling(update, []liveDogfoodCommand{unannotated})
	require.NotNil(t, got)
	assert.Equal(t, []string{"issues", "show"}, got.Path)
}

func TestSplitLiveDogfoodInvocation(t *testing.T) {
	t.Parallel()

	positionals, flags := splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "1", "--subject=renamed", "--json", "--closed", "-s", "other"},
		writeReadbackUpdateHelp,
	)
	assert.Equal(t, []string{"1"}, positionals)
	assert.Equal(t, "other", flags["subject"])
	assert.Equal(t, "true", flags["json"])
	assert.Equal(t, "true", flags["closed"])

	positionals, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "--subject", "renamed", "--note-id", "n1", "7"},
		writeReadbackUpdateHelp,
	)
	assert.Equal(t, []string{"7"}, positionals)
	assert.Equal(t, "renamed", flags["subject"])
	assert.Equal(t, "n1", flags["note-id"])

	positionals, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "1", "--closed=false", "--", "--subject=hidden"},
		writeReadbackUpdateHelp,
	)
	assert.Equal(t, []string{"1", "--subject=hidden"}, positionals)
	assert.Equal(t, "false", flags["closed"])

	positionals, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "1", "--subject", "--renamed", "--subject", "--", "--json"},
		writeReadbackUpdateHelp,
	)
	assert.Equal(t, []string{"1"}, positionals)
	assert.Equal(t, "--", flags["subject"])
	assert.Equal(t, "true", flags["json"])

	positionals, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "1", "-s", "--", "--closed"},
		writeReadbackUpdateHelp,
	)
	assert.Equal(t, []string{"1"}, positionals)
	assert.Equal(t, "--", flags["subject"])
	assert.Equal(t, "true", flags["closed"])

	_, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "1", "-s=renamed", "-sother"},
		writeReadbackUpdateHelp,
	)
	assert.Equal(t, "other", flags["subject"])

	configHelp := writeReadbackUpdateHelp + "  -c, --config string   Config file\n"
	_, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "1", "-c"},
		configHelp,
	)
	assert.Empty(t, flags["config"])
	conn, connReason := writeReadbackConnectionArgs(
		liveDogfoodCommand{Path: []string{"issues", "update"}, Help: configHelp},
		[]string{"issues", "update", "1", "-c"},
	)
	assert.Nil(t, conn)
	assert.Equal(t, reasonWriteReadbackConnection, connReason)

	_, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "1", "--subject"},
		writeReadbackUpdateHelp,
	)
	_, hasSubject := flags["subject"]
	assert.False(t, hasSubject, "a missing value is not the boolean true")

	_, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "1", "--closed", "--subject=renamed"},
		writeReadbackUpdateHelp,
	)
	assert.Equal(t, "true", flags["closed"])
	assert.Equal(t, "renamed", flags["subject"])

	sliceHelp := writeReadbackUpdateHelp + "      --tag strings   Tags\n"
	_, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "1", "--tag=new", "--tag=old", "--subject=renamed"},
		sliceHelp,
	)
	_, hasTag := flags["tag"]
	assert.False(t, hasTag, "a repeated slice is not one comparable value")
	assert.Equal(t, "renamed", flags["subject"])

	_, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "1", "--tag=only"},
		sliceHelp,
	)
	assert.Equal(t, "only", flags["tag"])

	positionals, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "--config", "other.toml", "--insecure=false", "1"},
		writeReadbackUpdateHelp,
	)
	assert.Equal(t, []string{"1"}, positionals)
	assert.Equal(t, "other.toml", flags["config"])
	assert.Equal(t, "false", flags["insecure"])

	positionals, flags = splitLiveDogfoodInvocation(
		[]string{"issues", "update"},
		[]string{"issues", "update", "--insecure", "1"},
		writeReadbackUpdateHelp,
	)
	assert.Equal(t, []string{"1"}, positionals)
	assert.Equal(t, "true", flags["insecure"])

	cmd := liveDogfoodCommand{Path: []string{"issues", "update"}, Help: writeReadbackUpdateHelp}
	conn, connReason = writeReadbackConnectionArgs(cmd, []string{"issues", "update", "--config="})
	assert.Nil(t, conn)
	assert.Equal(t, reasonWriteReadbackConnection, connReason)
	conn, connReason = writeReadbackConnectionArgs(cmd, []string{"issues", "update", "1", "--insecure"})
	assert.Equal(t, []string{"--insecure=true"}, conn)
	assert.Empty(t, connReason)
}

func TestLiveDogfoodSubmittedFields(t *testing.T) {
	t.Parallel()

	cmd := liveDogfoodCommand{Path: []string{"issues", "update"}, Help: writeReadbackUpdateHelp}
	fields, names := liveDogfoodSubmittedFields(cmd, []string{"issues", "update", "1", "--subject=renamed", "--json", "--note-id=n1", "--id=1"}, nil)
	assert.Equal(t, map[string]string{"noteid": "n1", "subject": "renamed"}, fields)
	assert.Equal(t, "subject", names["subject"])
	_, echoedID := fields["id"]
	assert.False(t, echoedID, "the resource id is not a submitted body field")

	addressed := liveDogfoodCommand{Path: []string{"notes", "update"}, Help: "Usage:\n  fixture-pp-cli notes update <note_id> [flags]\n\nFlags:\n      --note-id string\n      --body string\n"}
	fields, _ = liveDogfoodSubmittedFields(addressed, []string{"notes", "update", "n1", "--note-id=n1", "--body=hello"}, nil)
	assert.Equal(t, map[string]string{"body": "hello"}, fields)

	fields, _ = liveDogfoodSubmittedFields(cmd, []string{"issues", "update", "1"}, []byte(`{"id":"1","subject":"from-stdin"}`))
	assert.Equal(t, map[string]string{"subject": "from-stdin"}, fields)

	fields, names = liveDogfoodSubmittedFields(cmd, []string{"issues", "update", "1", "--status-id=3"}, nil)
	assert.Equal(t, "3", fields["statusid"])
	assert.Equal(t, "status-id", names["statusid"])

	fields, _ = liveDogfoodSubmittedFields(cmd, []string{"issues", "update", "1"}, []byte(`{"issue":{"subject":"from-stdin"}}`))
	assert.Equal(t, "from-stdin", fields["subject"])

	fields, _ = liveDogfoodSubmittedFields(cmd, []string{"issues", "update", "1", "--subject=flag"}, []byte(`{"subject":"stdin"}`))
	_, ok := fields["subject"]
	assert.False(t, ok, "disagreeing flag and stdin values are not comparable")

	fields, _ = liveDogfoodSubmittedFields(cmd, []string{"issues", "update", "1"}, []byte(`{"status":204,"success":true}`))
	assert.Empty(t, fields)

	fields, names = liveDogfoodSubmittedFields(cmd, []string{"issues", "update", "1"}, []byte(`{"status":"closed"}`))
	assert.Equal(t, "closed", fields["status"])
	assert.Equal(t, "status", names["status"])

	fields, _ = liveDogfoodSubmittedFields(cmd, []string{"issues", "update", "1"}, []byte(`{"issue":{"subject":"wrapped"},"other":{"name":"nope"}}`))
	assert.Empty(t, fields, "two nested objects are not unwrapped")

	fields, _ = liveDogfoodSubmittedFields(cmd, []string{"issues", "update", "1", "--config=other.toml", "--profile=work", "--api-key=secret", "--subject=renamed"}, nil)
	assert.Equal(t, map[string]string{"subject": "renamed"}, fields)
}

func TestBuildWriteReadbackArgs(t *testing.T) {
	t.Parallel()

	args, reason := buildWriteReadbackArgs([]string{"issues", "get"}, writeReadbackGetHelp, []string{"proj", "1"}, nil)
	assert.Empty(t, reason)
	assert.Equal(t, []string{"issues", "get", "1", "--json"}, args)

	nestedHelp := `Get a task.

Usage:
  fixture-pp-cli projects tasks get <project_id> <task_id> [flags]

Flags:
      --json   Output JSON
`
	args, reason = buildWriteReadbackArgs([]string{"projects", "tasks", "get"}, nestedHelp, []string{"proj", "task"}, nil)
	assert.Empty(t, reason)
	assert.Equal(t, []string{"projects", "tasks", "get", "proj", "task", "--json"}, args)

	optionalHelp := `Get an issue.

Usage:
  fixture-pp-cli issues get <id> [note]

Flags:
      --json   Output JSON
`
	args, reason = buildWriteReadbackArgs([]string{"issues", "get"}, optionalHelp, []string{"1"}, nil)
	assert.Empty(t, reason)
	assert.Equal(t, []string{"issues", "get", "1", "--json"}, args)

	_, reason = buildWriteReadbackArgs([]string{"issues", "get"}, nestedHelp, []string{"only"}, nil)
	assert.Equal(t, reasonWriteReadbackNoID, reason)

	args, reason = buildWriteReadbackArgs([]string{"issues", "get"}, writeReadbackGetByFlagHelp, []string{"1"}, nil)
	assert.Empty(t, reason)
	assert.Equal(t, []string{"issues", "get", "--id=1", "--json"}, args)

	_, reason = buildWriteReadbackArgs([]string{"issues", "get"}, writeReadbackGetByFlagHelp, nil, nil)
	assert.Equal(t, reasonWriteReadbackNoID, reason)

	noJSON := `Get an issue.

Usage:
  fixture-pp-cli issues get <id>
`
	args, reason = buildWriteReadbackArgs([]string{"issues", "get"}, noJSON, []string{"1"}, nil)
	assert.Empty(t, reason)
	assert.Equal(t, []string{"issues", "get", "1"}, args)

	connection := []string{"--api-key=secret", "--base-url=https://example.test", "--config=other.toml", "--insecure=false", "--profile=work"}
	args, reason = buildWriteReadbackArgs([]string{"issues", "get"}, writeReadbackGetHelp, []string{"1"}, connection)
	assert.Empty(t, reason)
	assert.Equal(t, append([]string{"issues", "get", "1"}, append(connection, "--json")...), args)
	assert.NotContains(t, args, "--subject=renamed")
}

func TestExtractCreatedID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "top level", raw: `{"id":"5","subject":"new"}`, want: "5", ok: true},
		{name: "wrapped", raw: `{"status":201,"success":true,"issue":{"id":9,"subject":"new"}}`, want: "9", ok: true},
		{name: "numeric", raw: `{"id":12}`, want: "12", ok: true},
		{name: "uppercase id", raw: `{"ID":"abc"}`, want: "abc", ok: true},
		{name: "agreeing id spellings", raw: `{"id":"1","ID":"1"}`, want: "1", ok: true},
		{name: "disagreeing id spellings", raw: `{"id":"1","ID":"2","nested":{"id":"9"}}`},
		{name: "foreign key is not the resource id", raw: `{"issueId":"abc"}`},
		{name: "trailing junk", raw: `{"id":"1"}{"id":"2"}`},
		{name: "newline conflicting records", raw: "{\"id\":\"1\"}\n{\"id\":\"2\"}"},
		{name: "newline agreeing records", raw: "{\"id\":\"1\"}\n{\"id\":\"1\"}"},
		{name: "log line then one record", raw: "warning: retry\n{\"id\":\"9\"}", want: "9", ok: true},
		{name: "envelope only", raw: `{"status":204,"success":true}`},
		{name: "ambiguous", raw: `{"a":{"id":"1"},"b":{"id":"2"}}`},
		{name: "same nested id", raw: `{"a":{"id":"1"},"b":{"id":"1"}}`, want: "1", ok: true},
		{name: "empty id", raw: `{"id":""}`},
		{name: "not json", raw: `not-json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			decoded, ok := decodeLiveDogfoodJSONValue(tt.raw)
			if !ok {
				assert.False(t, tt.ok)
				return
			}
			got, found := extractCreatedID(decoded)
			assert.Equal(t, tt.ok, found)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestJudgeWriteReadback(t *testing.T) {
	t.Parallel()

	names := map[string]string{"subject": "subject", "description": "description"}
	before := mustDecodeReadback(t, `{"issue":{"id":"1","subject":"old","description":"same","updated_on":"2020-01-01T00:00:00Z"}}`)

	tests := []struct {
		name   string
		fields map[string]string
		before any
		after  string
		status LiveDogfoodStatus
		reason string
	}{
		{
			name: "exact nested match", fields: map[string]string{"subject": "renamed"}, before: before,
			after:  `{"issue":{"id":"1","subject":"renamed","updated_on":"2020-01-01T00:00:00Z"}}`,
			status: LiveDogfoodStatusPass,
		},
		{
			name: "unchanged field fails", fields: map[string]string{"subject": "renamed"}, before: before,
			after:  `{"issue":{"id":"1","subject":"old","updated_on":"2020-01-01T00:00:00Z"}}`,
			status: LiveDogfoodStatusFail,
			reason: `mutation reported success but read-back shows field "subject" unchanged`,
		},
		{
			name: "timestamp advance passes when the field is unchanged", fields: map[string]string{"subject": "renamed"}, before: before,
			after:  `{"issue":{"subject":"old","updated_on":"2024-01-01T00:00:00Z"}}`,
			status: LiveDogfoodStatusPass,
		},
		{
			name: "disagreeing clocks do not excuse an unchanged field", fields: map[string]string{"subject": "renamed"},
			before: mustDecodeReadback(t, `{"subject":"old","updated_on":"2020-01-01T00:00:00Z","modified_at":"2024-01-01T00:00:00Z"}`),
			after:  `{"subject":"old","updated_on":"2024-01-01T00:00:00Z","modified_at":"2020-01-01T00:00:00Z"}`,
			status: LiveDogfoodStatusFail,
			reason: `mutation reported success but read-back shows field "subject" unchanged`,
		},
		{
			name: "unparseable clock stays a failure", fields: map[string]string{"subject": "renamed"},
			before: mustDecodeReadback(t, `{"subject":"old","updated_on":"nope"}`),
			after:  `{"subject":"old","updated_on":"still-nope"}`,
			status: LiveDogfoodStatusFail,
		},
		{
			name: "camel case clock counts", fields: map[string]string{"subject": "renamed"},
			before: mustDecodeReadback(t, `{"subject":"old","updatedAt":"2020-01-01T00:00:00Z"}`),
			after:  `{"subject":"old","updatedAt":"2024-01-01T00:00:00Z"}`,
			status: LiveDogfoodStatusPass,
		},
		{
			name: "normalized case is skipped", fields: map[string]string{"subject": "renamed"}, before: before,
			after:  `{"subject":"Renamed"}`,
			status: LiveDogfoodStatusSkip, reason: reasonWriteReadbackNormalized,
		},
		{
			name: "trimmed value is skipped", fields: map[string]string{"subject": "renamed"}, before: before,
			after:  `{"subject":" renamed "}`,
			status: LiveDogfoodStatusSkip, reason: reasonWriteReadbackNormalized,
		},
		{
			name: "transformed value is skipped", fields: map[string]string{"subject": "renamed"}, before: before,
			after:  `{"subject":"renamed-slug"}`,
			status: LiveDogfoodStatusSkip, reason: reasonWriteReadbackTransformed,
		},
		{
			name: "absent field is skipped", fields: map[string]string{"subject": "renamed"}, before: before,
			after:  `{"issue":{"id":"1"}}`,
			status: LiveDogfoodStatusSkip, reason: reasonWriteReadbackAbsent,
		},
		{
			name: "ambiguous repeated field is absent", fields: map[string]string{"subject": "renamed"}, before: before,
			after:  `{"subject":"renamed","comments":[{"subject":"other"}]}`,
			status: LiveDogfoodStatusSkip, reason: reasonWriteReadbackAbsent,
		},
		{
			name: "one matching field passes", fields: map[string]string{"subject": "renamed", "description": "changed"}, before: before,
			after:  `{"subject":"renamed","description":"same"}`,
			status: LiveDogfoodStatusPass,
		},
		{
			name: "numeric equivalence passes", fields: map[string]string{"subject": "2"},
			before: mustDecodeReadback(t, `{"subject":"1"}`),
			after:  `{"subject":2}`,
			status: LiveDogfoodStatusPass,
		},
		{
			name: "decimal and integer equivalence passes", fields: map[string]string{"subject": "2.0"},
			before: mustDecodeReadback(t, `{"subject":"1"}`),
			after:  `{"subject":2}`,
			status: LiveDogfoodStatusPass,
		},
		{
			name: "identical integers above 2^53 pass", fields: map[string]string{"subject": "9007199254740993"},
			before: mustDecodeReadback(t, `{"subject":"1"}`),
			after:  `{"subject":9007199254740993}`,
			status: LiveDogfoodStatusPass,
		},
		{
			name: "neighboring integers above 2^53 stay unchanged", fields: map[string]string{"subject": "9007199254740993"},
			before: mustDecodeReadback(t, `{"subject":9007199254740992}`),
			after:  `{"subject":9007199254740992}`,
			status: LiveDogfoodStatusFail,
			reason: `mutation reported success but read-back shows field "subject" unchanged`,
		},
		{
			name: "bool equivalence passes", fields: map[string]string{"closed": "true"},
			before: mustDecodeReadback(t, `{"closed":false}`),
			after:  `{"closed":true}`,
			status: LiveDogfoodStatusPass,
		},
		{
			name: "null still holding empty fails", fields: map[string]string{"subject": "renamed"},
			before: mustDecodeReadback(t, `{"subject":null}`),
			after:  `{"subject":null}`,
			status: LiveDogfoodStatusFail,
		},
		{
			name: "no baseline mismatch is transformed", fields: map[string]string{"subject": "renamed"},
			after:  `{"subject":"server-default"}`,
			status: LiveDogfoodStatusSkip, reason: reasonWriteReadbackTransformed,
		},
		{
			name: "no fields", fields: map[string]string{},
			after:  `{"subject":"renamed"}`,
			status: LiveDogfoodStatusSkip, reason: reasonWriteReadbackNoField,
		},
		{
			name: "two unchanged fields name the first", fields: map[string]string{"subject": "renamed", "description": "changed"},
			before: before,
			after:  `{"subject":"old","description":"same"}`,
			status: LiveDogfoodStatusFail,
			reason: `mutation reported success but read-back shows field "description" unchanged`,
		},
		{
			name:   "a transformed sibling keeps an unchanged field inconclusive",
			fields: map[string]string{"subject": "renamed", "description": "changed"},
			before: before,
			after:  `{"subject":"renamed-slug","description":"same"}`,
			status: LiveDogfoodStatusSkip, reason: reasonWriteReadbackTransformed,
		},
		{
			name:   "a normalized sibling keeps an unchanged field inconclusive",
			fields: map[string]string{"subject": "renamed", "description": "changed"},
			before: before,
			after:  `{"subject":"Renamed","description":"same"}`,
			status: LiveDogfoodStatusSkip, reason: reasonWriteReadbackNormalized,
		},
		{
			name: "unix timestamp advance passes", fields: map[string]string{"subject": "renamed"},
			before: mustDecodeReadback(t, `{"subject":"old","updated_at":"1700000000"}`),
			after:  `{"subject":"old","updated_at":"1700000001"}`,
			status: LiveDogfoodStatusPass,
		},
		{
			name: "bool word equivalence passes", fields: map[string]string{"closed": "1"},
			before: mustDecodeReadback(t, `{"closed":false}`),
			after:  `{"closed":"true"}`,
			status: LiveDogfoodStatusPass,
		},
		{
			name: "short number is not a timestamp", fields: map[string]string{"subject": "renamed"},
			before: mustDecodeReadback(t, `{"subject":"old","updated_at":"12"}`),
			after:  `{"subject":"old","updated_at":"13"}`,
			status: LiveDogfoodStatusFail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			after := mustDecodeReadback(t, tt.after)
			status, reason := judgeWriteReadback(tt.fields, names, tt.before, after)
			assert.Equal(t, tt.status, status)
			if tt.reason != "" {
				assert.Equal(t, tt.reason, reason)
			} else if status == LiveDogfoodStatusPass {
				assert.Empty(t, reason)
			}
		})
	}
}

func TestStartLiveDogfoodWriteReadback(t *testing.T) {
	t.Parallel()

	update := liveDogfoodCommand{
		Path:        []string{"issues", "update"},
		Help:        writeReadbackUpdateHelp,
		Annotations: map[string]string{"pp:method": "PUT"},
	}
	get := liveDogfoodCommand{Path: []string{"issues", "get"}, Annotations: map[string]string{"pp:method": "GET"}}
	ctx := resolveCtx{siblings: map[string][]liveDogfoodCommand{"issues": {get, update}}}
	args := []string{"issues", "update", "1", "--subject=renamed"}
	beforeBody := `{"issue":{"id":"1","subject":"old","updated_on":"2020-01-01T00:00:00Z"}}`
	afterBody := `{"issue":{"id":"1","subject":"renamed","updated_on":"2020-01-01T00:00:00Z"}}`
	happy := LiveDogfoodTestResult{Status: LiveDogfoodStatusPass}

	t.Run("persisted update passes", func(t *testing.T) {
		t.Parallel()
		reads := 0
		session := startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, false, scriptedReadbackExec(t, &reads, beforeBody, afterBody))
		require.NotNil(t, session)
		got := session.finish(happy, liveDogfoodRun{stdout: `{"status":204,"success":true}`}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
		assert.Equal(t, LiveDogfoodTestWriteReadback, got.Kind)
		assert.Equal(t, []string{"issues", "get", "1", "--json"}, got.Args)
		assert.Equal(t, 2, reads)
	})

	t.Run("ignored update fails", func(t *testing.T) {
		t.Parallel()
		reads := 0
		session := startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, false, scriptedReadbackExec(t, &reads, beforeBody, beforeBody))
		got := session.finish(happy, liveDogfoodRun{stdout: `{"status":204,"success":true}`}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusFail, got.Status)
		assert.Equal(t, `mutation reported success but read-back shows field "subject" unchanged`, got.Reason)
	})

	t.Run("no sibling is skipped", func(t *testing.T) {
		t.Parallel()
		called := false
		exec := func(args []string, _ int) liveDogfoodRun {
			called = true
			return liveDogfoodRun{}
		}
		lonely := resolveCtx{siblings: map[string][]liveDogfoodCommand{"issues": {update}}}
		session := startLiveDogfoodWriteReadbackWith(update, lonely, args, nil, false, exec)
		require.NotNil(t, session)
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusSkip, got.Status)
		assert.Equal(t, reasonWriteReadbackNoSibling, got.Reason)
		assert.False(t, called)
	})

	t.Run("sibling help failure is skipped", func(t *testing.T) {
		t.Parallel()
		session := startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, false, func(args []string, _ int) liveDogfoodRun {
			return liveDogfoodRun{exitCode: 2, stderr: "boom"}
		})
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusSkip, got.Status)
		assert.Equal(t, reasonWriteReadbackReadFailed+" (exit 2)", got.Reason)
	})

	t.Run("sibling read failure is skipped", func(t *testing.T) {
		t.Parallel()
		calls := 0
		session := startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, false, func(got []string, _ int) liveDogfoodRun {
			calls++
			if strings.HasSuffix(strings.Join(got, " "), "--help") {
				return liveDogfoodRun{stdout: writeReadbackGetHelp}
			}
			if calls == 2 {
				return liveDogfoodRun{stdout: beforeBody}
			}
			return liveDogfoodRun{exitCode: 5, stderr: "unavailable"}
		})
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusSkip, got.Status)
		assert.Equal(t, reasonWriteReadbackReadFailed+" (exit 5)", got.Reason)
		assert.Contains(t, got.OutputSample, "unavailable")
	})

	t.Run("setup failure without an error is skipped", func(t *testing.T) {
		t.Parallel()
		session := startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, false, func(got []string, _ int) liveDogfoodRun {
			if strings.Contains(strings.Join(got, " "), "--help") {
				return liveDogfoodRun{stdout: writeReadbackGetHelp}
			}
			return liveDogfoodRun{setupFailure: true}
		})
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, reasonWriteReadbackReadFailed, got.Reason)
	})

	t.Run("setup failure is skipped", func(t *testing.T) {
		t.Parallel()
		session := startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, false, func(got []string, _ int) liveDogfoodRun {
			if strings.Contains(strings.Join(got, " "), "--help") {
				return liveDogfoodRun{stdout: writeReadbackGetHelp}
			}
			return liveDogfoodRun{exitCode: -1, setupFailure: true, err: assertErr("create live dogfood scratch dir: denied")}
		})
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusSkip, got.Status)
		assert.Contains(t, got.Reason, "denied")
	})

	t.Run("non json read is skipped", func(t *testing.T) {
		t.Parallel()
		session := startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, false, func(got []string, _ int) liveDogfoodRun {
			if strings.Contains(strings.Join(got, " "), "--help") {
				return liveDogfoodRun{stdout: writeReadbackGetHelp}
			}
			return liveDogfoodRun{stdout: "Issue 1 subject=old"}
		})
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusSkip, got.Status)
		assert.Equal(t, reasonWriteReadbackNotJSON, got.Reason)
	})

	t.Run("field absent is skipped", func(t *testing.T) {
		t.Parallel()
		session := startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, false, scriptedReadbackExec(t, new(int), `{"issue":{"id":"1"}}`, `{"issue":{"id":"1","updated_on":"2024-01-01T00:00:00Z"}}`))
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusSkip, got.Status)
		assert.Equal(t, reasonWriteReadbackAbsent, got.Reason)
	})

	t.Run("no submitted field is skipped", func(t *testing.T) {
		t.Parallel()
		session := startLiveDogfoodWriteReadbackWith(update, ctx, []string{"issues", "update", "1", "--json"}, nil, false, func(got []string, _ int) liveDogfoodRun {
			return liveDogfoodRun{stdout: writeReadbackGetHelp}
		})
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusSkip, got.Status)
		assert.Equal(t, reasonWriteReadbackNoField, got.Reason)
	})

	t.Run("id flag read", func(t *testing.T) {
		t.Parallel()
		flagged := liveDogfoodCommand{
			Path:        []string{"issues", "update"},
			Help:        "Usage:\n  fixture-pp-cli issues update [flags]\n\nFlags:\n      --id string\n      --subject string\n",
			Annotations: map[string]string{"pp:method": "PUT"},
		}
		session := startLiveDogfoodWriteReadbackWith(flagged, ctx, []string{"issues", "update", "--id=1", "--subject=renamed"}, nil, false, func(got []string, _ int) liveDogfoodRun {
			joined := strings.Join(got, " ")
			if strings.Contains(joined, "--help") {
				return liveDogfoodRun{stdout: writeReadbackGetByFlagHelp}
			}
			assert.Contains(t, got, "--id=1")
			return liveDogfoodRun{stdout: afterBody}
		})
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
	})

	t.Run("missing id is skipped", func(t *testing.T) {
		t.Parallel()
		session := startLiveDogfoodWriteReadbackWith(update, ctx, []string{"issues", "update", "--subject=renamed"}, nil, false, func(got []string, _ int) liveDogfoodRun {
			return liveDogfoodRun{stdout: writeReadbackGetHelp}
		})
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, reasonWriteReadbackNoID, got.Reason)
	})

	t.Run("failed happy path emits nothing", func(t *testing.T) {
		t.Parallel()
		reads := 0
		session := startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, false, scriptedReadbackExec(t, &reads, beforeBody, afterBody))
		got := session.finish(LiveDogfoodTestResult{Status: LiveDogfoodStatusFail}, liveDogfoodRun{}, "")
		assert.Nil(t, got)
		assert.Equal(t, 1, reads, "a failed write is not re-fetched")
	})

	t.Run("dry run result emits nothing", func(t *testing.T) {
		t.Parallel()
		session := startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, false, scriptedReadbackExec(t, new(int), beforeBody, beforeBody))
		got := session.finish(LiveDogfoodTestResult{Status: LiveDogfoodStatusPass, requestsDryRun: true}, liveDogfoodRun{}, "")
		assert.Nil(t, got)
	})

	t.Run("baseline read failure does not fail a later mismatch", func(t *testing.T) {
		t.Parallel()
		calls := 0
		session := startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, false, func(got []string, _ int) liveDogfoodRun {
			calls++
			if strings.Contains(strings.Join(got, " "), "--help") {
				return liveDogfoodRun{stdout: writeReadbackGetHelp}
			}
			if calls == 2 {
				return liveDogfoodRun{exitCode: 1}
			}
			return liveDogfoodRun{stdout: `{"subject":"server-default"}`}
		})
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusSkip, got.Status)
		assert.Equal(t, reasonWriteReadbackTransformed, got.Reason)
	})

	t.Run("create id is re-fetched", func(t *testing.T) {
		t.Parallel()
		create := liveDogfoodCommand{
			Path:        []string{"issues", "create"},
			Help:        "Usage:\n  fixture-pp-cli issues create [flags]\n\nFlags:\n      --subject string\n      --json\n",
			Annotations: map[string]string{"pp:method": "POST"},
		}
		var gotArgs []string
		session := startLiveDogfoodWriteReadbackWith(create, ctx, []string{"issues", "create", "--subject=renamed"}, nil, false, func(got []string, _ int) liveDogfoodRun {
			if strings.Contains(strings.Join(got, " "), "--help") {
				return liveDogfoodRun{stdout: writeReadbackGetHelp}
			}
			gotArgs = append([]string{}, got...)
			return liveDogfoodRun{stdout: afterBody}
		})
		result := session.finish(happy, liveDogfoodRun{stdout: `{"status":201,"success":true,"issue":{"id":9}}`}, "")
		require.NotNil(t, result)
		assert.Equal(t, LiveDogfoodStatusPass, result.Status, result.Reason)
		assert.Equal(t, []string{"issues", "get", "9", "--json"}, gotArgs)
	})

	t.Run("create without id is skipped", func(t *testing.T) {
		t.Parallel()
		create := liveDogfoodCommand{
			Path:        []string{"issues", "create"},
			Help:        "Usage:\n  fixture-pp-cli issues create [flags]\n\nFlags:\n      --subject string\n",
			Annotations: map[string]string{"pp:method": "POST"},
		}
		reads := 0
		session := startLiveDogfoodWriteReadbackWith(create, ctx, []string{"issues", "create", "--subject=renamed"}, nil, false, func(got []string, _ int) liveDogfoodRun {
			if strings.Contains(strings.Join(got, " "), "--help") {
				return liveDogfoodRun{stdout: writeReadbackGetHelp}
			}
			reads++
			return liveDogfoodRun{stdout: afterBody}
		})
		result := session.finish(happy, liveDogfoodRun{stdout: `{"status":201,"success":true}`}, "")
		require.NotNil(t, result)
		assert.Equal(t, reasonWriteReadbackNoID, result.Reason)
		assert.Equal(t, 0, reads)
	})

	t.Run("ambiguous create id is skipped", func(t *testing.T) {
		t.Parallel()
		create := liveDogfoodCommand{
			Path:        []string{"issues", "create"},
			Help:        "Usage:\n  fixture-pp-cli issues create [flags]\n\nFlags:\n      --subject string\n",
			Annotations: map[string]string{"pp:method": "POST"},
		}
		session := startLiveDogfoodWriteReadbackWith(create, ctx, []string{"issues", "create", "--subject=renamed"}, nil, false, func(got []string, _ int) liveDogfoodRun {
			return liveDogfoodRun{stdout: writeReadbackGetHelp}
		})
		result := session.finish(happy, liveDogfoodRun{stdout: `{"a":{"id":"1"},"b":{"id":"2"}}`}, "")
		require.NotNil(t, result)
		assert.Equal(t, reasonWriteReadbackNoID, result.Reason)
	})

	t.Run("stdin body is the submitted field", func(t *testing.T) {
		t.Parallel()
		session := startLiveDogfoodWriteReadbackWith(update, ctx, []string{"issues", "update", "1"}, []byte(`{"issue":{"subject":"renamed"}}`), false, scriptedReadbackExec(t, new(int), beforeBody, afterBody))
		got := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, got)
		assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
	})

	t.Run("dry-run and preview do not start", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, startLiveDogfoodWriteReadbackWith(update, ctx, append(args, "--dry-run"), nil, false, nil))
		assert.Nil(t, startLiveDogfoodWriteReadbackWith(update, ctx, args, nil, true, nil))
	})

	t.Run("connection flags are copied onto both reads", func(t *testing.T) {
		t.Parallel()
		var got [][]string
		session := startLiveDogfoodWriteReadbackWith(update, ctx, []string{
			"issues", "update", "1", "--config", "other.toml", "--profile=work",
			"--base-url=https://example.test", "--api-key=secret", "--insecure=false", "--subject=renamed",
		}, nil, false, func(gotArgs []string, _ int) liveDogfoodRun {
			got = append(got, append([]string{}, gotArgs...))
			if strings.Contains(strings.Join(gotArgs, " "), "--help") {
				return liveDogfoodRun{stdout: writeReadbackGetHelp}
			}
			return liveDogfoodRun{stdout: afterBody}
		})
		result := session.finish(happy, liveDogfoodRun{stdout: `{"status":204}`}, "")
		require.NotNil(t, result)
		assert.Equal(t, LiveDogfoodStatusPass, result.Status, result.Reason)
		require.Len(t, got, 3)
		for _, call := range got[1:] {
			assert.Contains(t, call, "--config=other.toml")
			assert.Contains(t, call, "--profile=work")
			assert.Contains(t, call, "--base-url=https://example.test")
			assert.Contains(t, call, "--api-key=secret")
			assert.Contains(t, call, "--insecure=false")
			assert.Contains(t, call, "1")
			assert.NotContains(t, call, "other.toml")
			assert.NotContains(t, call, "--subject=renamed")
		}
	})

	t.Run("missing connection value is skipped", func(t *testing.T) {
		t.Parallel()
		reads := 0
		session := startLiveDogfoodWriteReadbackWith(update, ctx, []string{"issues", "update", "1", "--subject=renamed", "--config"}, nil, false, func(got []string, _ int) liveDogfoodRun {
			if strings.Contains(strings.Join(got, " "), "--help") {
				return liveDogfoodRun{stdout: writeReadbackGetHelp}
			}
			reads++
			return liveDogfoodRun{stdout: beforeBody}
		})
		result := session.finish(happy, liveDogfoodRun{}, "")
		require.NotNil(t, result)
		assert.Equal(t, LiveDogfoodStatusSkip, result.Status)
		assert.Equal(t, reasonWriteReadbackConnection, result.Reason)
		assert.Equal(t, 0, reads)
	})

	t.Run("create read keeps connection flags", func(t *testing.T) {
		t.Parallel()
		create := liveDogfoodCommand{
			Path:        []string{"issues", "create"},
			Help:        "Usage:\n  fixture-pp-cli issues create [flags]\n\nFlags:\n      --subject string\n      --json\n",
			Annotations: map[string]string{"pp:method": "POST"},
		}
		var gotArgs []string
		session := startLiveDogfoodWriteReadbackWith(create, ctx, []string{"issues", "create", "--config=other.toml", "--subject=renamed"}, nil, false, func(got []string, _ int) liveDogfoodRun {
			if strings.Contains(strings.Join(got, " "), "--help") {
				return liveDogfoodRun{stdout: writeReadbackGetHelp}
			}
			gotArgs = append([]string{}, got...)
			return liveDogfoodRun{stdout: afterBody}
		})
		result := session.finish(happy, liveDogfoodRun{stdout: `{"id":"9","subject":"renamed"}`}, "")
		require.NotNil(t, result)
		assert.Equal(t, LiveDogfoodStatusPass, result.Status, result.Reason)
		assert.Equal(t, []string{"issues", "get", "9", "--config=other.toml", "--json"}, gotArgs)
	})
}

func TestSubmittedNumbersMatch(t *testing.T) {
	t.Parallel()

	assert.True(t, submittedNumbersMatch("2", "2.0"))
	assert.True(t, submittedNumbersMatch("1e2", "100"))
	assert.True(t, submittedNumbersMatch("9007199254740993", "9007199254740993"))
	assert.False(t, submittedNumbersMatch("9007199254740992", "9007199254740993"))
	assert.False(t, submittedNumbersMatch("abc", "1"))
	assert.False(t, submittedNumbersMatch("", "1"))
	assert.False(t, submittedNumbersMatch("Inf", "Inf"))
	assert.False(t, submittedNumbersMatch("NaN", "1"))
}

func TestDecodeLiveDogfoodJSONValue(t *testing.T) {
	t.Parallel()

	value, ok := decodeLiveDogfoodJSONValue("  {\"id\":\"1\"}\n")
	assert.True(t, ok)
	decoded, isMap := value.(map[string]any)
	require.True(t, isMap)
	assert.Equal(t, "1", decoded["id"])

	_, ok = decodeLiveDogfoodJSONValue("{\"id\":\"1\"}\n{\"id\":\"2\"}")
	assert.False(t, ok)

	_, ok = decodeLiveDogfoodJSONValue("not json\nstill not")
	assert.False(t, ok)

	_, ok = decodeLiveDogfoodJSONValue("   ")
	assert.False(t, ok)

	value, ok = decodeLiveDogfoodJSONValue("log line\n{\"id\":\"3\"}\n")
	assert.True(t, ok)
	decoded, isMap = value.(map[string]any)
	require.True(t, isMap)
	assert.Equal(t, "3", decoded["id"])
}

func TestRunLiveDogfoodWriteReadbackMatrix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a python fixture binary")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required")
	}

	tests := []struct {
		name         string
		acceptFlat   bool
		omitSubject  bool
		withGet      bool
		allow        bool
		wantStatus   LiveDogfoodStatus
		wantReason   string
		wantVerdict  string
		wantSubject  string
		wantNoResult bool
	}{
		{
			name: "persisted update passes", acceptFlat: true, withGet: true, allow: true,
			wantStatus: LiveDogfoodStatusPass, wantVerdict: "PASS", wantSubject: "renamed",
		},
		{
			name: "ignored flat body fails", acceptFlat: false, withGet: true, allow: true,
			wantStatus: LiveDogfoodStatusFail, wantVerdict: "FAIL", wantSubject: "before",
			wantReason: `mutation reported success but read-back shows field "subject" unchanged`,
		},
		{
			name: "absent field is skipped", acceptFlat: true, omitSubject: true, withGet: true, allow: true,
			wantStatus: LiveDogfoodStatusSkip, wantReason: reasonWriteReadbackAbsent, wantVerdict: "PASS", wantSubject: "renamed",
		},
		{
			name: "no sibling read is skipped", acceptFlat: true, withGet: false, allow: true,
			wantStatus: LiveDogfoodStatusSkip, wantReason: reasonWriteReadbackNoSibling, wantVerdict: "PASS", wantSubject: "renamed",
		},
		{
			name: "skipped mutator has no read-back row", acceptFlat: true, withGet: true, allow: false,
			wantVerdict: "PASS", wantSubject: "before", wantNoResult: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newReadbackIssueStore(tt.acceptFlat, tt.omitSubject)
			server := httptest.NewServer(store)
			t.Cleanup(server.Close)
			t.Setenv("PRINTING_PRESS_TEST_ISSUE_URL", server.URL)
			if tt.withGet {
				t.Setenv("PRINTING_PRESS_TEST_ISSUE_WITH_GET", "1")
			} else {
				t.Setenv("PRINTING_PRESS_TEST_ISSUE_WITH_GET", "0")
			}
			dir := t.TempDir()
			binaryName := "fixture-pp-cli"
			writeTestManifestForLiveDogfood(t, dir)
			require.NoError(t, os.WriteFile(filepath.Join(dir, binaryName), []byte(writeReadbackFixtureScript), 0o755))

			report, err := RunLiveDogfood(LiveDogfoodOptions{
				CLIDir:           dir,
				BinaryName:       binaryName,
				Level:            "full",
				Timeout:          10 * time.Second,
				AllowDestructive: tt.allow,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantVerdict, report.Verdict, report.Tests)
			got := findResultByCommandKind(report, "issues update", LiveDogfoodTestWriteReadback)
			if tt.wantNoResult {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, tt.wantStatus, got.Status)
				if tt.wantReason != "" {
					assert.Equal(t, tt.wantReason, got.Reason)
				}
			}
			happy := findResultByCommandKind(report, "issues update", LiveDogfoodTestHappy)
			require.NotNil(t, happy)
			if tt.allow {
				assert.Equal(t, LiveDogfoodStatusPass, happy.Status, happy.Reason)
			}
			assert.Equal(t, tt.wantSubject, store.subject())
			if !tt.withGet {
				assert.NotContains(t, store.methods(), http.MethodGet)
			}
		})
	}
}

func scriptedReadbackExec(t *testing.T, reads *int, before, after string) liveDogfoodReadbackExec {
	t.Helper()
	return func(args []string, _ int) liveDogfoodRun {
		if strings.Contains(strings.Join(args, " "), "--help") {
			return liveDogfoodRun{stdout: writeReadbackGetHelp}
		}
		*reads++
		if *reads == 1 {
			return liveDogfoodRun{stdout: before}
		}
		return liveDogfoodRun{stdout: after}
	}
}

func mustDecodeReadback(t *testing.T, raw string) any {
	t.Helper()
	value, ok := decodeLiveDogfoodJSONValue(raw)
	require.True(t, ok, raw)
	return value
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

type readbackIssueStore struct {
	mu          sync.Mutex
	current     string
	acceptFlat  bool
	omitSubject bool
	seen        []string
}

func newReadbackIssueStore(acceptFlat, omitSubject bool) *readbackIssueStore {
	return &readbackIssueStore{current: "before", acceptFlat: acceptFlat, omitSubject: omitSubject}
}

func (s *readbackIssueStore) subject() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *readbackIssueStore) methods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.seen...)
}

func (s *readbackIssueStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, r.Method)
	id := strings.TrimPrefix(r.URL.Path, "/issues/")
	if id == "__printing_press_invalid__" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		issue := map[string]any{"id": id, "updated_on": "2020-01-01T00:00:00Z"}
		if !s.omitSubject {
			issue["subject"] = s.current
		}
		writeReadbackJSON(w, map[string]any{"issue": issue})
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if next := readbackAppliedSubject(body, s.acceptFlat); next != "" {
			s.current = next
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func readbackAppliedSubject(body []byte, acceptFlat bool) string {
	var wrapped struct {
		Issue struct {
			Subject string `json:"subject"`
		} `json:"issue"`
	}
	if json.Unmarshal(body, &wrapped) == nil && wrapped.Issue.Subject != "" {
		return wrapped.Issue.Subject
	}
	if !acceptFlat {
		return ""
	}
	var flat struct {
		Subject string `json:"subject"`
	}
	if json.Unmarshal(body, &flat) == nil {
		return flat.Subject
	}
	return ""
}

func writeReadbackJSON(w http.ResponseWriter, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(encoded)
}

const writeReadbackFixtureScript = `#!/usr/bin/env python3
import json, os, sys, urllib.error, urllib.parse, urllib.request

args = sys.argv[1:]
base = os.environ["PRINTING_PRESS_TEST_ISSUE_URL"]

def help_text(name):
    if name == "get":
        usage = "fixture-pp-cli issues get <id> [flags]"
        example = "fixture-pp-cli issues get 1"
        flags = "      --json   Output JSON\n"
    else:
        usage = "fixture-pp-cli issues update <id> [flags]"
        example = "fixture-pp-cli issues update 1 --subject=renamed"
        flags = "      --subject string   New subject\n      --json   Output JSON\n"
    return "Issue command.\n\nUsage:\n  %s\n\nExamples:\n  %s\n\nFlags:\n%s" % (usage, example, flags)

if args[:1] == ["agent-context"]:
    include_get = os.environ.get("PRINTING_PRESS_TEST_ISSUE_WITH_GET", "1") == "1"
    commands = []
    if include_get:
        commands.append('{"name":"get","annotations":{"pp:method":"GET","pp:path":"/issues/{id}","pp:happy-args":"id=1"}}')
    commands.append('{"name":"update","annotations":{"pp:method":"PUT","pp:path":"/issues/{id}","pp:live-happy-path":"true","pp:happy-args":"id=1;--subject=renamed"}}')
    sys.stdout.write('{"commands":[{"name":"issues","subcommands":[%s]}]}' % ",".join(commands))
    raise SystemExit(0)

if len(args) >= 3 and args[0] == "issues" and args[2] == "--help":
    sys.stdout.write(help_text(args[1]))
    raise SystemExit(0)

def parsed(argv):
    ident = ""
    subject = None
    i = 0
    while i < len(argv):
        arg = argv[i]
        if arg.startswith("--subject="):
            subject = arg.split("=", 1)[1]
        elif arg == "--subject" and i + 1 < len(argv):
            i += 1
            subject = argv[i]
        elif arg in ("--json", "--help", "issues", "get", "update"):
            pass
        elif not arg.startswith("-"):
            ident = arg
        i += 1
    return ident, subject

def request(method, ident, payload=None):
    url = base + "/issues/" + urllib.parse.quote(ident)
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(url, data=data, method=method)
    try:
        with urllib.request.urlopen(req) as resp:
            return resp.read().decode(), 0
    except urllib.error.HTTPError as exc:
        sys.stderr.write(exc.read().decode())
        return "", 1

if len(args) >= 2 and args[0] == "issues" and args[1] == "get":
    ident, _ = parsed(args)
    body, code = request("GET", ident)
    if code != 0:
        raise SystemExit(code)
    sys.stdout.write(body)
    raise SystemExit(0)

if len(args) >= 2 and args[0] == "issues" and args[1] == "update":
    ident, subject = parsed(args)
    _, code = request("PUT", ident, {"subject": subject or ""})
    if code != 0:
        raise SystemExit(code)
    sys.stdout.write('{"status":204,"success":true}')
    raise SystemExit(0)

sys.stderr.write("unexpected args: %s\n" % " ".join(args))
raise SystemExit(99)
`
