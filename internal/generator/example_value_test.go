package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

		// Browser-sniff customer-data shapes route to canonical synthetic placeholders.
		{"asin", spec.Param{Name: "asin", Type: "string"}, "B0EXAMPLE1"},
		{"card last four", spec.Param{Name: "card_last4", Type: "string"}, "LAST4"},
		{"recipient", spec.Param{Name: "recipient_name", Type: "string"}, "Test User"},
		{"address", spec.Param{Name: "shipping_address", Type: "string"}, "123 Test St, Anytown, ST 12345"},
		{"generic order id stays uuid", spec.Param{Name: "orderId", Type: "string"}, uuid},
		{"address id stays uuid", spec.Param{Name: "shipping_address_id", Type: "string"}, uuid},
		{"recipient id stays uuid", spec.Param{Name: "recipient_id", Type: "string"}, uuid},

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

// TestExampleValueEnumWinsOverNameHeuristics covers enum-aware placeholder
// selection. When a param carries enum constraints the API will reject any
// value outside that set, so enum[0] must beat the name-based branches that
// would otherwise emit `example-value` / `example-resource` / a UUID.
func TestExampleValueEnumWinsOverNameHeuristics(t *testing.T) {
	tests := []struct {
		name  string
		param spec.Param
		want  string
	}{
		// Petstore find-by-status: bare `status` with enum → enum[0], not example-value.
		{"status string enum picks first", spec.Param{Name: "status", Type: "string", Enum: []string{"available", "pending", "sold"}}, "available"},
		// Petstore find-by-tags: even array-typed enum params take enum[0] (per
		// CLI flag parsing — a single value is always a legal subset of the array).
		{"tags array enum picks first", spec.Param{Name: "tags", Type: "array", Enum: []string{"cat", "dog", "rabbit"}}, "cat"},
		// Empty/whitespace entries are skipped; first non-empty value wins.
		{"enum skips empty entries", spec.Param{Name: "mode", Type: "string", Enum: []string{"", " ", "deep"}}, "deep"},
		// Enum wins over name-shape heuristic. `*Id` would otherwise route to UUID.
		{"enum beats id-shape", spec.Param{Name: "statusId", Type: "string", Enum: []string{"a", "b"}}, "a"},
		// Enum wins over the `name`/`title` branch that returns `example-resource`.
		{"enum beats name branch", spec.Param{Name: "displayName", Type: "string", Enum: []string{"alpha", "beta"}}, "alpha"},
		// PII-synthetic shapes (asin, card_last4, recipient, address) take
		// priority over enum: the synthetic placeholder protects browser-sniff
		// runs from echoing captured customer data, and enum constraints on
		// those fields are vanishingly rare in practice. If a real API ever
		// hits this case, the synthetic value is wrong but safe; enum-driven
		// placeholders would be right but risk re-emitting captured PII.
		{"synthetic asin beats enum", spec.Param{Name: "asin", Type: "string", Enum: []string{"X1", "X2"}}, "B0EXAMPLE1"},
		// Empty enum slice is a no-op; fall through to default behavior.
		{"empty enum falls through", spec.Param{Name: "status", Type: "string", Enum: nil}, "example-value"},
		// Non-string enums (the OpenAPI parser still populates Enum as []string;
		// the placeholder doesn't need conversion logic — the printed CLI's
		// validator handles parsing).
		{"numeric enum still string-stringified", spec.Param{Name: "priority", Type: "integer", Enum: []string{"1", "2", "3"}}, "1"},
	}

	// Sanity: confirm the asin synthetic placeholder hasn't moved out from
	// under the assertion above. If syntheticExampleValue ever stops handling
	// asin, the test below loses its meaning (would silently become an
	// enum-wins assertion instead of a synthetic-beats-enum assertion).
	syntheticASIN, ok := syntheticExampleValue("asin")
	require.True(t, ok, "syntheticExampleValue must continue to recognize 'asin'; otherwise the synthetic-beats-enum test below loses its meaning")
	require.Equal(t, "B0EXAMPLE1", syntheticASIN)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exampleValue(tt.param)
			assert.Equal(t, tt.want, got)
		})
	}
}
