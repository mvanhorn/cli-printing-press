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

// TestBodyMap_WireName verifies that when a Param declares WireName the
// generated body key uses WireName (the Salesforce SObject field name) not
// the snake_case CLI flag Name. Go variable name still derives from Name.
// This is Patch 3: salesforce-wire-names.
func TestBodyMap_WireName(t *testing.T) {
	t.Parallel()
	// developer_name flag → DeveloperName SObject key
	got := bodyMap([]spec.Param{{Name: "developer_name", WireName: "DeveloperName", Type: "string"}}, "\t")
	if !strings.Contains(got, "bodyDeveloperName") {
		t.Errorf("expected Go var to camelCase Name, got: %s", got)
	}
	if !strings.Contains(got, `body["DeveloperName"]`) {
		t.Errorf("expected wire key DeveloperName, got: %s", got)
	}
	if strings.Contains(got, `body["developer_name"]`) {
		t.Errorf("must NOT use snake_case key when WireName set, got: %s", got)
	}
}

// TestBodyMap_WireName_FallsBackToName verifies no regression: params without
// WireName still use Name as the body key.
func TestBodyMap_WireName_FallsBackToName(t *testing.T) {
	t.Parallel()
	got := bodyMap([]spec.Param{{Name: "description", Type: "string"}}, "\t")
	if !strings.Contains(got, `body["description"]`) {
		t.Errorf("expected body key to be Name when no WireName, got: %s", got)
	}
}

// TestBodyMap_InlineAtRoot verifies that a Param with InlineAtRoot:true and
// Type:"object" generates code that merges parsed fields directly into body
// (not wrapped under body["name"] = parsed). This is Patch 4:
// salesforce-fields-inline — Salesforce SObject PATCH expects fields at root.
func TestBodyMap_InlineAtRoot(t *testing.T) {
	t.Parallel()
	got := bodyMap([]spec.Param{{Name: "fields", Type: "object", InlineAtRoot: true}}, "\t")
	// Must parse JSON into a typed map for iteration
	if !strings.Contains(got, "parsedFields") {
		t.Errorf("expected parsedFields variable, got: %s", got)
	}
	// Must merge each key from parsedFields into body, not assign whole blob
	if !strings.Contains(got, "body[k] = v") {
		t.Errorf("expected inline merge body[k]=v, got: %s", got)
	}
	// Must NOT assign body["fields"] = parsedFields (that wraps, breaks Salesforce)
	if strings.Contains(got, `body["fields"]`) {
		t.Errorf("must NOT wrap under body[\"fields\"] when InlineAtRoot=true, got: %s", got)
	}
}
