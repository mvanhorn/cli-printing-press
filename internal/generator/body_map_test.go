package generator

import (
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
