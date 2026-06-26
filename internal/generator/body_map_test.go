package generator

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// TestBodyMap pins the rendered Go code for each of the three body-param
// shapes (object/array, JSON-string, scalar). The generator's golden
// harness only exercises the scalar branch, so this test guards the
// other two against silent drift after the bash → helper extraction.
func TestBodyMap(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		body   []spec.Param
		indent string
		want   string
	}{
		{
			name:   "scalar string",
			body:   []spec.Param{{Name: "name", Type: "string"}},
			indent: "\t\t\t\t",
			want: "\t\t\t\tif bodyName != \"\" {\n" +
				"\t\t\t\t\tbody[\"name\"] = bodyName\n" +
				"\t\t\t\t}\n",
		},
		{
			name:   "scalar int",
			body:   []spec.Param{{Name: "count", Type: "int"}},
			indent: "\t\t\t",
			want: "\t\t\tif bodyCount != 0 {\n" +
				"\t\t\t\tbody[\"count\"] = bodyCount\n" +
				"\t\t\t}\n",
		},
		{
			// Booleans gate on cmd.Flags().Changed instead of the
			// scalar zero-guard so user-set false reaches the wire
			// AND untouched flags don't overwrite server state on
			// PATCH endpoints. See issue #1298.
			name:   "scalar boolean (internal spec form) gates on Changed, not zero-guard",
			body:   []spec.Param{{Name: "private", Type: "boolean"}},
			indent: "\t\t\t",
			want: "\t\t\tif cmd.Flags().Changed(\"private\") {\n" +
				"\t\t\t\tbody[\"private\"] = bodyPrivate\n" +
				"\t\t\t}\n",
		},
		{
			// The OpenAPI parser normalizes "boolean" -> "bool", so the
			// renderer must match both forms or fail open on OpenAPI specs.
			name:   "scalar bool (OpenAPI-normalized form) gates on Changed",
			body:   []spec.Param{{Name: "enabled", Type: "bool"}},
			indent: "\t\t\t",
			want: "\t\t\tif cmd.Flags().Changed(\"enabled\") {\n" +
				"\t\t\t\tbody[\"enabled\"] = bodyEnabled\n" +
				"\t\t\t}\n",
		},
		{
			name:   "object branch parses JSON and stores parsed value",
			body:   []spec.Param{{Name: "metadata", Type: "object"}},
			indent: "\t\t\t",
			want: "\t\t\tif bodyMetadata != \"\" {\n" +
				"\t\t\t\tvar parsedMetadata any\n" +
				"\t\t\t\tif err := json.Unmarshal([]byte(bodyMetadata), &parsedMetadata); err != nil {\n" +
				"\t\t\t\t\treturn fmt.Errorf(\"parsing --metadata JSON: %w\", err)\n" +
				"\t\t\t\t}\n" +
				"\t\t\t\tbody[\"metadata\"] = parsedMetadata\n" +
				"\t\t\t}\n",
		},
		{
			name:   "array branch matches object branch shape",
			body:   []spec.Param{{Name: "tags", Type: "array"}},
			indent: "\t\t\t",
			want: "\t\t\tif bodyTags != \"\" {\n" +
				"\t\t\t\tvar parsedTags any\n" +
				"\t\t\t\tif err := json.Unmarshal([]byte(bodyTags), &parsedTags); err != nil {\n" +
				"\t\t\t\t\treturn fmt.Errorf(\"parsing --tags JSON: %w\", err)\n" +
				"\t\t\t\t}\n" +
				"\t\t\t\tbody[\"tags\"] = parsedTags\n" +
				"\t\t\t}\n",
		},
		{
			// JSON-string params: type is "string" but the format/description
			// signal JSON content. The branch validates JSON before sending
			// but stores the raw string (not the parsed value) so the API
			// receives the user's exact bytes.
			name:   "jsonString branch validates but stores raw",
			body:   []spec.Param{{Name: "config", Type: "string", Format: "json"}},
			indent: "\t\t\t",
			want: "\t\t\tif bodyConfig != \"\" {\n" +
				"\t\t\t\tvar parsedConfig any\n" +
				"\t\t\t\tif err := json.Unmarshal([]byte(bodyConfig), &parsedConfig); err != nil {\n" +
				"\t\t\t\t\treturn fmt.Errorf(\"parsing --config JSON: %w\", err)\n" +
				"\t\t\t\t}\n" +
				"\t\t\t\tbody[\"config\"] = bodyConfig\n" +
				"\t\t\t}\n",
		},
		{
			// Polymorphic body fields (for example oneOf scalar-or-object)
			// accept either a scalar string or a JSON object/array. JSON-looking
			// values must be parsed before entering the body map so the API sees
			// an object/array, not a quoted JSON string.
			name:   "json-or-scalar branch parses composite values and keeps scalar fallback",
			body:   []spec.Param{{Name: "response_engine", Type: "string", Format: "json_or_scalar"}},
			indent: "\t\t\t",
			want: "\t\t\tif bodyResponseEngine != \"\" {\n" +
				"\t\t\t\tif looksLikeJSONComposite(bodyResponseEngine) {\n" +
				"\t\t\t\t\tvar parsedResponseEngine any\n" +
				"\t\t\t\t\tif err := json.Unmarshal([]byte(bodyResponseEngine), &parsedResponseEngine); err != nil {\n" +
				"\t\t\t\t\t\treturn fmt.Errorf(\"parsing --response-engine JSON: %w\", err)\n" +
				"\t\t\t\t\t}\n" +
				"\t\t\t\t\tbody[\"response_engine\"] = parsedResponseEngine\n" +
				"\t\t\t\t} else {\n" +
				"\t\t\t\t\tbody[\"response_engine\"] = bodyResponseEngine\n" +
				"\t\t\t\t}\n" +
				"\t\t\t}\n",
		},
		{
			name: "multiple params concatenate in order",
			body: []spec.Param{
				{Name: "name", Type: "string"},
				{Name: "tags", Type: "array"},
			},
			indent: "\t",
			want: "\tif bodyName != \"\" {\n" +
				"\t\tbody[\"name\"] = bodyName\n" +
				"\t}\n" +
				"\tif bodyTags != \"\" {\n" +
				"\t\tvar parsedTags any\n" +
				"\t\tif err := json.Unmarshal([]byte(bodyTags), &parsedTags); err != nil {\n" +
				"\t\t\treturn fmt.Errorf(\"parsing --tags JSON: %w\", err)\n" +
				"\t\t}\n" +
				"\t\tbody[\"tags\"] = parsedTags\n" +
				"\t}\n",
		},
		{
			// Required bools without a default are string-backed so the
			// CLI can distinguish omitted from explicit false. The body-map
			// block must parse the string back to bool before storing so
			// json.Marshal emits {"all_day":false}, not {"all_day":"false"}.
			name:   "required bool without default parses string-backed flag",
			body:   []spec.Param{{Name: "all_day", Type: "boolean", Required: true}},
			indent: "\t\t\t",
			want: "\t\t\tif bodyAllDay != \"\" {\n" +
				"\t\t\t\tparsedAllDay, err := strconv.ParseBool(bodyAllDay)\n" +
				"\t\t\t\tif err != nil {\n" +
				"\t\t\t\t\treturn fmt.Errorf(\"parsing --all-day as bool: %w\", err)\n" +
				"\t\t\t\t}\n" +
				"\t\t\t\tbody[\"all_day\"] = parsedAllDay\n" +
				"\t\t\t}\n",
		},
		{
			name:   "empty body produces empty string",
			body:   nil,
			indent: "\t\t\t",
			want:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bodyMap(tc.body, tc.indent)
			if got != tc.want {
				t.Errorf("bodyMap mismatch.\n got:\n%s\nwant:\n%s\nraw got: %q\nraw want: %q",
					got, tc.want, got, tc.want)
			}
		})
	}
}

// TestBodyMap_DashIdentifier verifies hyphenated param names route through
// paramIdent + camelCase the same way the templates do — `user-id` becomes
// `bodyUserId` (for the variable) but stays `user-id` in the JSON key.
func TestBodyMap_DashIdentifier(t *testing.T) {
	t.Parallel()
	got := bodyMap([]spec.Param{{Name: "user-id", Type: "string"}}, "\t")
	if !strings.Contains(got, "bodyUserId") {
		t.Errorf("expected camelCased identifier, got: %s", got)
	}
	if !strings.Contains(got, `body["user-id"]`) {
		t.Errorf("expected JSON key with dash preserved, got: %s", got)
	}
}

// TestBodyMap_IdentName verifies the dedup pass's output: when IdentName
// is set (because two params would otherwise collide on the same Go
// identifier), the variable name uses IdentName but body[key] keeps the
// wire Name. Without this, the generated CLI would either fail to compile
// or send the wrong field name to the server.
func TestBodyMap_IdentName(t *testing.T) {
	t.Parallel()
	got := bodyMap([]spec.Param{{Name: "start", IdentName: "StartGT", Type: "string"}}, "\t")
	if !strings.Contains(got, "bodyStartGT") {
		t.Errorf("expected variable to use IdentName, got: %s", got)
	}
	if !strings.Contains(got, `body["start"]`) {
		t.Errorf("expected wire key to use Name (not IdentName), got: %s", got)
	}
}

func TestBodyMap_BodyNameOverridesJSONKey(t *testing.T) {
	t.Parallel()
	got := bodyMap([]spec.Param{{Name: "startAfter", BodyName: "searchAfter", Type: "array"}}, "\t")
	if !strings.Contains(got, "bodyStartAfter") {
		t.Errorf("expected public name to drive variable identity, got: %s", got)
	}
	if !strings.Contains(got, `body["searchAfter"] = parsedStartAfter`) {
		t.Errorf("expected body_name to drive JSON key, got: %s", got)
	}
	if strings.Contains(got, `body["startAfter"]`) {
		t.Errorf("public name must not leak as JSON key when body_name is set, got: %s", got)
	}
}

// TestBodyMap_NestedObject verifies that body params declaring
// type=object with non-empty Fields render a nested-map block in
// place of the JSON-string parse path. The wire key is the parent's
// Name; field keys are each leaf's Name. Field-flag variables are
// parent-prefixed (bodyStartDateTime, not bodyDateTime) so two
// parents that share a field name do not collide.
func TestBodyMap_NestedObject(t *testing.T) {
	t.Parallel()
	got := bodyMap([]spec.Param{{
		Name: "start",
		Type: "object",
		Fields: []spec.Param{
			{Name: "dateTime", Type: "string"},
			{Name: "timeZone", Type: "string"},
		},
	}}, "\t")
	want := "\t{\n" +
		"\t\tnestedStart := map[string]any{}\n" +
		"\t\tif bodyStartDateTime != \"\" {\n" +
		"\t\t\tnestedStart[\"dateTime\"] = bodyStartDateTime\n" +
		"\t\t}\n" +
		"\t\tif bodyStartTimeZone != \"\" {\n" +
		"\t\t\tnestedStart[\"timeZone\"] = bodyStartTimeZone\n" +
		"\t\t}\n" +
		"\t\tif len(nestedStart) > 0 {\n" +
		"\t\t\tbody[\"start\"] = nestedStart\n" +
		"\t\t}\n" +
		"\t}\n"
	if got != want {
		t.Errorf("bodyMap nested mismatch.\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestBodyMap_NestedObject_BooleanLeaf verifies the boolean Changed
// gate threads through the nested-object recursion: an untouched
// boolean leaf adds nothing to the inner map, so len(nestedMap) > 0
// stays false and the parent object key is not emitted. Without this,
// every PATCH whose body declares a nested object with a boolean leaf
// would silently send the parent with field=false on every call.
func TestBodyMap_NestedObject_BooleanLeaf(t *testing.T) {
	t.Parallel()
	got := bodyMap([]spec.Param{{
		Name: "settings",
		Type: "object",
		Fields: []spec.Param{
			{Name: "private", Type: "boolean"},
		},
	}}, "\t")
	want := "\t{\n" +
		"\t\tnestedSettings := map[string]any{}\n" +
		"\t\tif cmd.Flags().Changed(\"settings-private\") {\n" +
		"\t\t\tnestedSettings[\"private\"] = bodySettingsPrivate\n" +
		"\t\t}\n" +
		"\t\tif len(nestedSettings) > 0 {\n" +
		"\t\t\tbody[\"settings\"] = nestedSettings\n" +
		"\t\t}\n" +
		"\t}\n"
	if got != want {
		t.Errorf("bodyMap nested boolean leaf mismatch.\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestBodyMap_NestedObject_PreservesScalarSiblings verifies that
// nested and flat body params can coexist: nested produces a block,
// scalars keep their existing if-then-set form.
func TestBodyMap_NestedObject_PreservesScalarSiblings(t *testing.T) {
	t.Parallel()
	got := bodyMap([]spec.Param{
		{Name: "subject", Type: "string"},
		{Name: "start", Type: "object", Fields: []spec.Param{{Name: "dateTime", Type: "string"}}},
	}, "\t")
	if !strings.Contains(got, `if bodySubject != "" {`) {
		t.Errorf("scalar branch missing, got:\n%s", got)
	}
	if !strings.Contains(got, `body["subject"] = bodySubject`) {
		t.Errorf("scalar wire-set missing, got:\n%s", got)
	}
	if !strings.Contains(got, `nestedStart := map[string]any{}`) {
		t.Errorf("nested-map declaration missing, got:\n%s", got)
	}
	if !strings.Contains(got, `nestedStart["dateTime"] = bodyStartDateTime`) {
		t.Errorf("nested-field set missing, got:\n%s", got)
	}
}

// TestBodyMap_NestedObject_EmptyFieldsKeepsJSONStringPath verifies the
// non-recursive case: an object body param with no Fields keeps the
// existing JSON-string parse-and-store path so OpenAPI specs that lack
// nested-property metadata are unaffected.
func TestBodyMap_NestedObject_EmptyFieldsKeepsJSONStringPath(t *testing.T) {
	t.Parallel()
	got := bodyMap([]spec.Param{{Name: "metadata", Type: "object"}}, "\t")
	if !strings.Contains(got, "json.Unmarshal([]byte(bodyMetadata)") {
		t.Errorf("expected JSON-parse path for object without Fields, got:\n%s", got)
	}
	if strings.Contains(got, "nestedMetadata") {
		t.Errorf("object without Fields must not emit a nested-map block, got:\n%s", got)
	}
}

// TestBodyMap_NestedJSONStringLeafUsesParentPrefixedFlag verifies that
// when a leaf inside a parent's Fields is itself routed through the
// JSON-string parse path (an array, or an object without Fields), the
// emitted error message uses the parent-prefixed flag name. Without
// flagPrefix threading, the error would name only the leaf — misleading
// users who set `--metadata-tags` and saw "parsing --tags JSON" on
// failure.
func TestBodyMap_NestedJSONStringLeafUsesParentPrefixedFlag(t *testing.T) {
	t.Parallel()
	got := bodyMap([]spec.Param{{
		Name: "metadata",
		Type: "object",
		Fields: []spec.Param{
			{Name: "tags", Type: "array"},
		},
	}}, "\t")
	if !strings.Contains(got, `"parsing --metadata-tags JSON: %w"`) {
		t.Errorf("expected parent-prefixed flag in error message, got:\n%s", got)
	}
	if strings.Contains(got, `"parsing --tags JSON: %w"`) {
		t.Errorf("error must not name leaf-only flag (the registered flag is parent-prefixed), got:\n%s", got)
	}
}

// TestBodyMap_DeepNesting verifies that nesting recurses past one
// level. A spec where a parent.child both declare Fields should produce
// nested blocks two levels deep, with parent-prefixed identifiers
// flowing through unchanged.
func TestBodyMap_DeepNesting(t *testing.T) {
	t.Parallel()
	got := bodyMap([]spec.Param{{
		Name: "filter",
		Type: "object",
		Fields: []spec.Param{{
			Name: "range",
			Type: "object",
			Fields: []spec.Param{
				{Name: "min", Type: "int"},
				{Name: "max", Type: "int"},
			},
		}},
	}}, "\t")
	if !strings.Contains(got, "nestedFilter") || !strings.Contains(got, "nestedFilterRange") {
		t.Errorf("expected two-level nested-map declarations, got:\n%s", got)
	}
	if !strings.Contains(got, `nestedFilterRange["min"] = bodyFilterRangeMin`) {
		t.Errorf("expected parent-prefixed leaf set, got:\n%s", got)
	}
	if !strings.Contains(got, `nestedFilter["range"] = nestedFilterRange`) {
		t.Errorf("expected child map assigned to parent map, got:\n%s", got)
	}
}

// TestBodyVarDecls_Flat pins the flat-case output so existing CLIs
// (no nested fields) do not see any generator-output diff after the
// helper takes over from the inline `{{- range .Endpoint.Body}}` loop.
func TestBodyVarDecls_Flat(t *testing.T) {
	t.Parallel()
	got := bodyVarDecls(spec.Endpoint{
		Body: []spec.Param{
			{Name: "name", Type: "string"},
			{Name: "count", Type: "int"},
		},
	})
	want := "\n\tvar bodyName string\n\tvar bodyCount int"
	if got != want {
		t.Errorf("bodyVarDecls flat mismatch.\n got:%q\nwant:%q", got, want)
	}
}

// TestBodyVarDecls_Nested expands a single nested-object body param
// into one var per leaf field with parent-prefixed identifiers, and
// emits no var for the parent itself.
func TestBodyVarDecls_Nested(t *testing.T) {
	t.Parallel()
	got := bodyVarDecls(spec.Endpoint{
		Body: []spec.Param{{
			Name: "start",
			Type: "object",
			Fields: []spec.Param{
				{Name: "dateTime", Type: "string"},
				{Name: "timeZone", Type: "string"},
			},
		}},
	})
	want := "\n\tvar bodyStartDateTime string\n\tvar bodyStartTimeZone string"
	if got != want {
		t.Errorf("bodyVarDecls nested mismatch.\n got:%q\nwant:%q", got, want)
	}
	if strings.Contains(got, "bodyStart string") {
		t.Errorf("parent var must not be declared when Fields populated, got:%q", got)
	}
}

// TestBodyVarDecls_NonJSONStaysFlat verifies that multipart and
// form-encoded endpoints preserve the flat var-declaration shape so
// multipartBodyMaps and formBodyMaps (which serialize object-typed
// parents as JSON-string fields) still have the parent variable to read
// from.
func TestBodyVarDecls_NonJSONStaysFlat(t *testing.T) {
	t.Parallel()
	for _, contentType := range []string{"multipart/form-data", "application/x-www-form-urlencoded"} {
		got := bodyVarDecls(spec.Endpoint{
			RequestContentType: contentType,
			Body: []spec.Param{{
				Name:   "start",
				Type:   "object",
				Fields: []spec.Param{{Name: "dateTime", Type: "string"}},
			}},
		})
		want := "\n\tvar bodyStart string"
		if got != want {
			t.Errorf("[%s] bodyVarDecls must stay flat. got:%q want:%q", contentType, got, want)
		}
	}
}

// TestBodyFlagRegs_Flat pins the flat-case output for cobra flag
// registration. Aliases follow the primary registration with
// MarkHidden, mirroring the original template.
func TestBodyFlagRegs_Flat(t *testing.T) {
	t.Parallel()
	got := bodyFlagRegs(spec.Endpoint{
		Body: []spec.Param{
			{Name: "name", Type: "string", Description: "Display name", Aliases: []string{"n"}},
		},
	})
	want := "\n\tcmd.Flags().StringVar(&bodyName, \"name\", \"\", \"Display name\")" +
		"\n\tcmd.Flags().StringVar(&bodyName, \"n\", \"\", \"Display name\")" +
		"\n\t_ = cmd.Flags().MarkHidden(\"n\")"
	if got != want {
		t.Errorf("bodyFlagRegs flat mismatch.\n got:%q\nwant:%q", got, want)
	}
}

// TestBodyFlagRegs_Nested registers one flag per leaf field with
// parent-prefixed flag names so two parents that share a field name
// (e.g. start.dateTime + end.dateTime) do not collide. Aliases are not
// propagated to nested fields.
func TestBodyFlagRegs_Nested(t *testing.T) {
	t.Parallel()
	got := bodyFlagRegs(spec.Endpoint{
		Body: []spec.Param{{
			Name:        "start",
			Type:        "object",
			Description: "Start of window",
			Aliases:     []string{"s"},
			Fields: []spec.Param{
				{Name: "dateTime", Type: "string", Description: "RFC3339 timestamp"},
				{Name: "timeZone", Type: "string", Description: "IANA zone"},
			},
		}},
	})
	if !strings.Contains(got, "cmd.Flags().StringVar(&bodyStartDateTime, \"start-date-time\", \"\", \"RFC3339 timestamp\")") {
		t.Errorf("expected parent-prefixed flag for nested dateTime, got:\n%s", got)
	}
	if !strings.Contains(got, "cmd.Flags().StringVar(&bodyStartTimeZone, \"start-time-zone\", \"\", \"IANA zone\")") {
		t.Errorf("expected parent-prefixed flag for nested timeZone, got:\n%s", got)
	}
	if strings.Contains(got, "cmd.Flags().StringVar(&bodyStart, \"start\"") {
		t.Errorf("parent flag must not be registered when Fields populated, got:\n%s", got)
	}
	if strings.Contains(got, "MarkHidden") {
		t.Errorf("parent aliases must not propagate to nested fields, got:\n%s", got)
	}
}

// TestBodyFlagRegs_NonJSONStaysFlat verifies multipart and form-encoded
// endpoints keep the parent JSON-string flag because their body-map
// helpers serialize object-typed parents as a single JSON string.
func TestBodyFlagRegs_NonJSONStaysFlat(t *testing.T) {
	t.Parallel()
	for _, contentType := range []string{"multipart/form-data", "application/x-www-form-urlencoded"} {
		got := bodyFlagRegs(spec.Endpoint{
			RequestContentType: contentType,
			Body: []spec.Param{{
				Name:   "start",
				Type:   "object",
				Fields: []spec.Param{{Name: "dateTime", Type: "string"}},
			}},
		})
		if !strings.Contains(got, "cmd.Flags().StringVar(&bodyStart, \"start\"") {
			t.Errorf("[%s] must keep parent flag, got:\n%s", contentType, got)
		}
		if strings.Contains(got, "bodyStartDateTime") {
			t.Errorf("[%s] must not emit nested flag, got:\n%s", contentType, got)
		}
	}
}

// TestBodyRequiredChecks_NestedField uses parent-prefixed flag in the
// emitted `cmd.Flags().Changed(...)` call so the validator agrees with
// the flag name registered in bodyFlagRegs.
func TestBodyRequiredChecks_NestedField(t *testing.T) {
	t.Parallel()
	got := bodyRequiredChecks(spec.Endpoint{
		Body: []spec.Param{{
			Name: "start",
			Type: "object",
			Fields: []spec.Param{
				{Name: "dateTime", Type: "string", Required: true},
			},
		}},
	}, "\t\t\t")
	if !strings.Contains(got, `cmd.Flags().Changed("start-date-time")`) {
		t.Errorf("expected parent-prefixed Changed() call for nested required field, got:\n%s", got)
	}
	if !strings.Contains(got, `"required flag \"%s\" not set", "start-date-time"`) {
		t.Errorf("expected parent-prefixed flag name in error message, got:\n%s", got)
	}
}

// TestBodyRequiredChecks_TopLevelKeepsAliasOR verifies that top-level
// required-flag checks still use flagChangedExpr (which ORs aliases).
// Without this, a user passing `--n value` would fail the required
// check even though `name` was effectively set.
func TestBodyRequiredChecks_TopLevelKeepsAliasOR(t *testing.T) {
	t.Parallel()
	got := bodyRequiredChecks(spec.Endpoint{
		Body: []spec.Param{
			{Name: "name", Type: "string", Required: true, Aliases: []string{"n"}},
		},
	}, "\t\t\t")
	if !strings.Contains(got, `(cmd.Flags().Changed("name") || cmd.Flags().Changed("n"))`) {
		t.Errorf("expected alias-OR in required check, got:\n%s", got)
	}
}

// TestBodyJSONFallback_VarDecls emits a single flagBodyJSON string and
// suppresses per-field var declarations when the endpoint opts into the
// oneOf/anyOf fallback.
func TestBodyJSONFallback_VarDecls(t *testing.T) {
	t.Parallel()
	got := bodyVarDecls(spec.Endpoint{BodyJSONFallback: true})
	want := "\n\tvar flagBodyJSON string"
	if got != want {
		t.Errorf("bodyVarDecls fallback mismatch.\n got:%q\nwant:%q", got, want)
	}
}

// TestBodyJSONFallback_FlagRegs registers a single --body-json flag with
// user-facing help that also names oneOf/anyOf for spec-aware readers.
func TestBodyJSONFallback_FlagRegs(t *testing.T) {
	t.Parallel()
	got := bodyFlagRegs(spec.Endpoint{BodyJSONFallback: true})
	if !strings.Contains(got, `cmd.Flags().StringVar(&flagBodyJSON, "body-json"`) {
		t.Errorf("expected --body-json flag registration, got:\n%s", got)
	}
	if !strings.Contains(got, "polymorphic schema") {
		t.Errorf("expected user-facing help text, got:\n%s", got)
	}
	if !strings.Contains(got, "oneOf/anyOf") {
		t.Errorf("expected spec-aware hint mentioning oneOf/anyOf, got:\n%s", got)
	}
}

// TestBodyJSONFallback_RequiredChecks emits no required-flag check
// because the parser cannot tell whether the request body is mandatory
// for an opaque schema. An empty body either succeeds or surfaces a
// clear 400 from the API.
func TestBodyJSONFallback_RequiredChecks(t *testing.T) {
	t.Parallel()
	got := bodyRequiredChecks(spec.Endpoint{BodyJSONFallback: true}, "\t\t\t")
	if got != "" {
		t.Errorf("bodyRequiredChecks should emit nothing for BodyJSONFallback, got:%q", got)
	}
}

// TestBodyJSONFallback_BodyMap dispatches bodyMapForEndpoint to the
// JSON-fallback renderer when the endpoint has BodyJSONFallback set.
// The block must parse the flag, reject non-object payloads, and
// overwrite the empty body map prepared by the caller.
func TestBodyJSONFallback_BodyMap(t *testing.T) {
	t.Parallel()
	got := bodyMapForEndpoint(spec.Endpoint{BodyJSONFallback: true}, "\t")
	wantSubstrings := []string{
		`if flagBodyJSON != ""`,
		`var parsedBodyJSON any`,
		`json.Unmarshal([]byte(flagBodyJSON), &parsedBodyJSON)`,
		`asMap, ok := parsedBodyJSON.(map[string]any)`,
		`body = asMap`,
		`--body-json must be a JSON object, got JSON %T`,
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(got, s) {
			t.Errorf("body-json fallback output missing %q, got:\n%s", s, got)
		}
	}
}

// TestBodyJSONFallback_BodyMap_TypedPath confirms bodyMapForEndpoint
// falls through to the typed renderer when BodyJSONFallback is false,
// preserving existing CLIs' generated output.
func TestBodyJSONFallback_BodyMap_TypedPath(t *testing.T) {
	t.Parallel()
	endpoint := spec.Endpoint{Body: []spec.Param{{Name: "name", Type: "string"}}}
	got := bodyMapForEndpoint(endpoint, "\t")
	if strings.Contains(got, "flagBodyJSON") {
		t.Errorf("typed-body path must not emit flagBodyJSON branch, got:\n%s", got)
	}
	if !strings.Contains(got, `body["name"] = bodyName`) {
		t.Errorf("expected typed body-map output for name field, got:\n%s", got)
	}
}

func TestBodyHasStringBackedBool(t *testing.T) {
	t.Parallel()
	endpoint := spec.Endpoint{Body: []spec.Param{{
		Name: "settings",
		Type: "object",
		Fields: []spec.Param{
			{Name: "all_day", Type: "bool", Required: true},
		},
	}}}
	if !bodyHasStringBackedBool(endpoint) {
		t.Fatal("expected nested required bool without default to require strconv import")
	}
	endpoint.BodyJSONFallback = true
	if bodyHasStringBackedBool(endpoint) {
		t.Fatal("body-json fallback bypasses typed bool parsing and must not require strconv")
	}
}

func TestNonJSONBodyMaps_RequiredBoolNoDefaultUsesStringZero(t *testing.T) {
	t.Parallel()
	body := []spec.Param{{Name: "all_day", Type: "boolean", Required: true}}
	multipart := multipartBodyMaps(body, "\t")
	if !strings.Contains(multipart, `if bodyAllDay != "" {`) {
		t.Errorf("multipart required bool must compare against string zero value, got:\n%s", multipart)
	}
	if strings.Contains(multipart, `bodyAllDay != false`) {
		t.Errorf("multipart required bool must not compare string var to bool false, got:\n%s", multipart)
	}
	form := formBodyMaps(body, "\t")
	if !strings.Contains(form, `if bodyAllDay != "" {`) {
		t.Errorf("form required bool must compare against string zero value, got:\n%s", form)
	}
	if strings.Contains(form, `bodyAllDay != false`) {
		t.Errorf("form required bool must not compare string var to bool false, got:\n%s", form)
	}
}

// TestMCPParamBindings_BodyJSONFallback inserts a single body_json
// binding with Location="body_json", mirroring the CLI surface. Parser
// invariant: Body is empty when BodyJSONFallback is set.
func TestMCPParamBindings_BodyJSONFallback(t *testing.T) {
	t.Parallel()
	endpoint := spec.Endpoint{
		BodyJSONFallback: true,
		Params:           []spec.Param{{Name: "zoneId", Type: "string"}},
	}
	bindings := mcpParamBindings(endpoint, "/zones/{zoneId}/records")

	var foundBodyJSON, foundTypedBody bool
	for _, b := range bindings {
		if b.Location == "body_json" && b.PublicName == "body_json" {
			foundBodyJSON = true
		}
		if b.Location == "body" {
			foundTypedBody = true
		}
	}
	if !foundBodyJSON {
		t.Errorf("expected a body_json binding, got: %+v", bindings)
	}
	if foundTypedBody {
		t.Errorf("BodyJSONFallback should suppress per-field body bindings, got: %+v", bindings)
	}
}

// TestBodyJSONFallback_RequiredChecks_RequiredBody emits a Changed check
// on --body-json when the OpenAPI requestBody.required flag was true.
func TestBodyJSONFallback_RequiredChecks_RequiredBody(t *testing.T) {
	t.Parallel()
	got := bodyRequiredChecks(spec.Endpoint{BodyJSONFallback: true, BodyRequired: true}, "\t\t\t")
	if !strings.Contains(got, `cmd.Flags().Changed("body-json")`) {
		t.Errorf("expected Changed check on body-json for required body, got:%q", got)
	}
	if !strings.Contains(got, `"required flag \"%s\" not set", "body-json"`) {
		t.Errorf("expected body-json in error message, got:%q", got)
	}
}

// deepBodyFixture builds a body with one root object whose Fields chain
// `levels` deep, ending in a string leaf. Each interior object has a
// scalar sibling so the truncation test can verify which depths emit and
// which are dropped.
//
//	body[level0Obj] (depth 0) ->
//	  level0Obj.sibling0 (string, depth 1 leaf)
//	  level0Obj.level1Obj (depth 1 object) ->
//	    level1Obj.sibling1 (string, depth 2 leaf)
//	    level1Obj.level2Obj (depth 2 object) ->
//	      level2Obj.sibling2 (string, depth 3 leaf, truncated at cap=3)
//	      level2Obj.level3Obj (depth 3 object, truncated at cap=3) -> ...
func deepBodyFixture(levels int) []spec.Param {
	if levels < 1 {
		return nil
	}
	// Build from the innermost leaf outward.
	current := []spec.Param{{Name: "leaf", Type: "string"}}
	for i := levels - 1; i >= 0; i-- {
		fields := append([]spec.Param{}, current...)
		fields = append([]spec.Param{{
			Name: "sibling" + strconv.Itoa(i),
			Type: "string",
		}}, fields...)
		current = []spec.Param{{
			Name:   "level" + strconv.Itoa(i) + "Obj",
			Type:   "object",
			Fields: fields,
		}}
	}
	return current
}

// TestBodyMap_DepthCap_TruncatesBelowMax verifies that body-map
// emission stops recursing into nested objects at maxBodyFlagDepth. A
// fixture nested 6 levels deep must produce per-field assignments only
// for depths 0..maxBodyFlagDepth-1; deeper subtrees are silently
// omitted (the user reaches them via --stdin).
func TestBodyMap_DepthCap_TruncatesBelowMax(t *testing.T) {
	t.Parallel()
	got := bodyMap(deepBodyFixture(6), "\t")

	// The depth-2 sibling and the depth-2 nested map block should both
	// appear (the cap allows three levels: 0, 1, 2).
	if !strings.Contains(got, "bodyLevel0ObjLevel1ObjSibling1") {
		t.Errorf("expected depth-2 sibling leaf to emit, got:\n%s", got)
	}
	if !strings.Contains(got, "nestedLevel0ObjLevel1Obj") {
		t.Errorf("expected depth-2 nested map block, got:\n%s", got)
	}
	// The depth-3 sibling, depth-3 object block, and any deeper identifiers
	// must be absent.
	if strings.Contains(got, "Sibling2") {
		t.Errorf("depth-3 sibling must be truncated by the cap, got:\n%s", got)
	}
	if strings.Contains(got, "nestedLevel0ObjLevel1ObjLevel2Obj") {
		t.Errorf("depth-3 nested map block must be truncated, got:\n%s", got)
	}
	if strings.Contains(got, "Level3Obj") || strings.Contains(got, "Leaf") {
		t.Errorf("anything below depth-2 must be omitted, got:\n%s", got)
	}
}

// TestBodyVarDecls_DepthCap pins the var-declaration set for a deep body.
// Only fields reachable within the cap should produce `var bodyX` lines.
func TestBodyVarDecls_DepthCap(t *testing.T) {
	t.Parallel()
	got := bodyVarDecls(spec.Endpoint{Body: deepBodyFixture(6)})
	for _, want := range []string{
		"\n\tvar bodyLevel0ObjSibling0 string",
		"\n\tvar bodyLevel0ObjLevel1ObjSibling1 string",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected within-cap var decl %q, got:\n%s", want, got)
		}
	}
	for _, banned := range []string{
		"bodyLevel0ObjLevel1ObjLevel2ObjSibling2",
		"bodyLevel0ObjLevel1ObjLevel2ObjLevel3Obj",
	} {
		if strings.Contains(got, banned) {
			t.Errorf("depth-capped identifier %q must not emit, got:\n%s", banned, got)
		}
	}
}

// TestBodyFlagRegs_DepthCap pins cobra flag registrations for a deep
// body: only flags reachable within the cap are registered.
func TestBodyFlagRegs_DepthCap(t *testing.T) {
	t.Parallel()
	got := bodyFlagRegs(spec.Endpoint{Body: deepBodyFixture(6)})
	if !strings.Contains(got, `"level0-obj-level1-obj-sibling1"`) {
		t.Errorf("expected depth-2 flag registration, got:\n%s", got)
	}
	if strings.Contains(got, "sibling2") {
		t.Errorf("depth-3 flag must be truncated, got:\n%s", got)
	}
}

// TestBodyMap_DepthCap_ShallowUnchanged ensures specs that fit inside
// the cap (2 levels of nesting) emit identical output to today: no
// truncation, every leaf reachable.
func TestBodyMap_DepthCap_ShallowUnchanged(t *testing.T) {
	t.Parallel()
	got := bodyMap(deepBodyFixture(2), "\t")
	for _, want := range []string{
		"bodyLevel0ObjSibling0",
		"bodyLevel0ObjLevel1ObjSibling1",
		"bodyLevel0ObjLevel1ObjLeaf",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("2-level fixture must emit %q with no truncation, got:\n%s", want, got)
		}
	}
}

// TestBodyExceedsFlagDepth_True reports truncation when the body nests
// past the cap. A 4-level fixture exceeds maxBodyFlagDepth=3.
func TestBodyExceedsFlagDepth_True(t *testing.T) {
	t.Parallel()
	if !bodyExceedsFlagDepth(spec.Endpoint{Body: deepBodyFixture(4)}) {
		t.Error("4-level body must report truncation under cap=3")
	}
}

// TestBodyExceedsFlagDepth_False reports no truncation when the body
// fits inside the cap.
func TestBodyExceedsFlagDepth_False(t *testing.T) {
	t.Parallel()
	if bodyExceedsFlagDepth(spec.Endpoint{Body: deepBodyFixture(2)}) {
		t.Error("2-level body must not report truncation under cap=3")
	}
	if bodyExceedsFlagDepth(spec.Endpoint{Body: []spec.Param{{Name: "n", Type: "string"}}}) {
		t.Error("flat body must not report truncation")
	}
}

// TestBodyExceedsFlagDepth_BodyJSONFallback never reports truncation
// for oneOf/anyOf bodies; those route through a single --body-json flag
// and never reach the per-field emitter.
func TestBodyExceedsFlagDepth_BodyJSONFallback(t *testing.T) {
	t.Parallel()
	if bodyExceedsFlagDepth(spec.Endpoint{BodyJSONFallback: true, Body: deepBodyFixture(6)}) {
		t.Error("BodyJSONFallback bypasses per-field emission and must not report truncation")
	}
}

// TestBodyExceedsFlagDepth_CollisionFlattenedSubtree pins the
// interaction with flattenCollidingBodyFields. When dot-flattened
// identifier collision clears an object's Fields, the emitters treat
// that subtree as a JSON-string leaf and never recurse past it -- no
// depth-cap truncation occurs. The predicate must read the same
// flattened tree the emitters render, or the --stdin help text reverts
// to the truncation-warning variant when every field is in fact a
// per-field flag.
func TestBodyExceedsFlagDepth_CollisionFlattenedSubtree(t *testing.T) {
	t.Parallel()
	// Top-level scalar 'outerInnerLeaf' collides with the dot-flattened
	// nested leaf outer.inner.leaf (both camelize to bodyOuterInnerLeaf
	// at depth 3). flattenCollidingBodyFields clears outer.Fields so the
	// emitters render outer as a JSON-string leaf at depth 0; no part of
	// the rendered tree exceeds the cap.
	body := []spec.Param{
		{Name: "outerInnerLeaf", Type: "string"},
		{Name: "outer", Type: "object", Fields: []spec.Param{
			{Name: "inner", Type: "object", Fields: []spec.Param{
				{Name: "leaf", Type: "string"},
			}},
		}},
	}
	if bodyExceedsFlagDepth(spec.Endpoint{Body: body}) {
		t.Error("collision-flattened subtree must not report truncation; emitters see a flat tree")
	}
}

// TestBodyExceedsFlagDepth_Multipart returns false for multipart
// endpoints regardless of nested-object depth: bodyUsesFlatEmission
// keeps multipart and form-encoded bodies one-flag-per-top-level-param,
// so deep nesting never triggers the per-field recursion the cap guards.
func TestBodyExceedsFlagDepth_Multipart(t *testing.T) {
	t.Parallel()
	endpoint := spec.Endpoint{
		Method:             "POST",
		RequestContentType: "multipart/form-data",
		Body:               deepBodyFixture(6),
	}
	if bodyExceedsFlagDepth(endpoint) {
		t.Error("multipart endpoint must not report truncation; flat emission never recurses")
	}
}

// TestBodyMap_DepthCap_Boundary pins the exact depth at which the cap
// fires. A fixture nested exactly maxBodyFlagDepth levels is the
// minimal spec that triggers truncation: the depth-2 object's children
// are at depth 3 and the recursion check (depth+1 >= maxBodyFlagDepth)
// stops the walk. The depth-1 sibling must still emit; the depth-2
// sibling and deeper leaves must be absent. A regression that toggled
// the check to `>` vs `>=` would flip this assertion.
func TestBodyMap_DepthCap_Boundary(t *testing.T) {
	t.Parallel()
	got := bodyMap(deepBodyFixture(maxBodyFlagDepth), "\t")
	if !strings.Contains(got, "bodyLevel0ObjSibling0") {
		t.Errorf("depth-1 sibling must emit at the boundary fixture, got:\n%s", got)
	}
	if !strings.Contains(got, "bodyLevel0ObjLevel1ObjSibling1") {
		t.Errorf("depth-2 sibling must emit at the boundary fixture, got:\n%s", got)
	}
	if strings.Contains(got, "Sibling2") || strings.Contains(got, "Leaf") {
		t.Errorf("boundary fixture must not emit depth-3 leaves, got:\n%s", got)
	}
}

// TestBodyRequiredChecks_DepthCap omits required-flag checks for fields
// truncated by the cap. The --stdin path bypasses required checks
// wholesale, so deep required fields are reachable via stdin only.
func TestBodyRequiredChecks_DepthCap(t *testing.T) {
	t.Parallel()
	// Build a deep fixture where the deepest sibling is required.
	body := deepBodyFixture(6)
	// Walk to the depth-3 sibling and mark it required.
	cursor := &body[0]
	for range 3 {
		// cursor.Fields[0] is "siblingN" (string leaf), cursor.Fields[1] is
		// "level<N+1>Obj" (the nested object). Descend the object branch.
		cursor = &cursor.Fields[1]
	}
	// cursor now points at level3Obj; mark its sibling (depth 4 leaf)
	// required.
	cursor.Fields[0].Required = true

	got := bodyRequiredChecks(spec.Endpoint{Body: body}, "\t\t\t")
	if strings.Contains(got, "sibling3") {
		t.Errorf("required check on a truncated deep leaf must not emit, got:\n%s", got)
	}
}
