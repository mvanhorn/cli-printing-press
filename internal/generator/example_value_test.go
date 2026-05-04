package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
)

// TestExampleValueIDRecognition covers the three id-shape clauses: bare `id`,
// snake_case `*_id`, and camelCase `*Id` (added with a string-type fence so
// boolean/numeric positionals like `paid` / `valid` don't get UUIDs).
func TestExampleValueIDRecognition(t *testing.T) {
	const uuid = "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name  string
		param spec.Param
		want  string
	}{
		// Bare id (existing behavior preserved).
		{"bare id string", spec.Param{Name: "id", Type: "string"}, uuid},
		{"bare ID uppercase", spec.Param{Name: "ID", Type: "string"}, uuid},
		{"bare Id pascal", spec.Param{Name: "Id", Type: "string"}, uuid},

		// snake_case (existing behavior preserved).
		{"snake movie_id", spec.Param{Name: "movie_id", Type: "string"}, uuid},
		{"snake user_id", spec.Param{Name: "user_id", Type: "string"}, uuid},

		// camelCase recognition (the new clause).
		{"camel movieId", spec.Param{Name: "movieId", Type: "string"}, uuid},
		{"camel seriesId", spec.Param{Name: "seriesId", Type: "string"}, uuid},
		{"camel personId", spec.Param{Name: "personId", Type: "string"}, uuid},
		{"camel pageId", spec.Param{Name: "pageId", Type: "string"}, uuid},
		{"camel issueId", spec.Param{Name: "issueId", Type: "string"}, uuid},

		// Empty Type passes the fence (legacy specs without type info).
		{"camel movieId no type", spec.Param{Name: "movieId"}, uuid},

		// Type fence: boolean and numeric positionals named *id flow to
		// their type branches, not UUID.
		{"bool paid", spec.Param{Name: "paid", Type: "boolean"}, "true"},
		{"bool valid", spec.Param{Name: "valid", Type: "boolean"}, "true"},
		{"bool unpaid", spec.Param{Name: "unpaid", Type: "boolean"}, "true"},
		{"bool creditValid", spec.Param{Name: "creditValid", Type: "boolean"}, "true"},
		{"int amountId", spec.Param{Name: "amountId", Type: "integer"}, "42"},
		// `countId` is integer-typed AND contains the substring "count",
		// so it lands in the count/limit/size branch (returns "50") rather
		// than the generic integer branch. Confirms the fence routes it
		// away from the UUID branch and into the existing type logic.
		{"int countId routes to count branch", spec.Param{Name: "countId", Type: "integer"}, "50"},

		// String-shaped alternative types (uuid, guid) ARE matched by the
		// not-numeric-or-bool fence. Spec authors emitting non-canonical
		// type strings get UUID examples just like canonical "string".
		{"camel movieId uuid type", spec.Param{Name: "movieId", Type: "uuid"}, uuid},
		{"camel personId guid type", spec.Param{Name: "personId", Type: "guid"}, uuid},

		// Negative — does not end in `id`.
		{"userIdentifier", spec.Param{Name: "userIdentifier", Type: "string"}, "example-value"},
		{"empty name", spec.Param{Name: "", Type: "string"}, "example-value"},
		{"too short ix", spec.Param{Name: "ix", Type: "string"}, "example-value"},
		{"too short id-but-len2 cd", spec.Param{Name: "cd", Type: "string"}, "example-value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exampleValue(tt.param)
			assert.Equal(t, tt.want, got)
		})
	}
}
