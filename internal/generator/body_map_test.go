package generator

import (
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
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
