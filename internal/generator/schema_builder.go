package generator

import (
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

type TableDef struct {
	// Name is the snake_cased SQL/Go identifier (table DDL, Pascal-derived
	// method names). Resource is the original spec key used by callers that
	// dispatch on the runtime resource string, which preserves spec casing.
	Name         string
	Resource     string
	Columns      []ColumnDef
	Indexes      []IndexDef
	FTS5         bool
	FTS5Fields   []string
	FTS5Triggers bool
}

type ColumnDef struct {
	Name       string
	Type       string
	PrimaryKey bool
	NotNull    bool
}

type IndexDef struct {
	Name      string
	TableName string
	Columns   string
	Unique    bool
}

// baseTableColumns are the three columns every domain table starts with.
// Used both as the initial column set and as the "already emitted" guard
// when extending the table with response-derived columns.
var baseTableColumns = []ColumnDef{
	{Name: "id", Type: "TEXT", PrimaryKey: true},
	{Name: "data", Type: "JSON", NotNull: true},
	{Name: "synced_at", Type: "DATETIME DEFAULT CURRENT_TIMESTAMP"},
}

// BuildSchema generates domain-specific table definitions from the API spec.
// High-gravity entities (many endpoints, text fields, temporal fields) get
// full column extraction. Low-gravity entities get simple id+data tables.
func BuildSchema(s *spec.APISpec) []TableDef {
	var tables []TableDef

	// Iterate resources in sorted order so generated output is deterministic
	// across runs and platforms — Go map iteration is randomized, and the
	// emitted switch statements in sync.go would otherwise drift between
	// generations and break golden verification.
	resourceNames := make([]string, 0, len(s.Resources))
	for name := range s.Resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)

	// Sharded names must agree with the profiler; both call SubResourceShardedNames.
	subResourceShards := spec.SubResourceShardedNames(s)

	for _, name := range resourceNames {
		resource := s.Resources[name]
		table := buildTable(s, name, resource)
		tables = append(tables, table)

		// Same determinism concern as the outer loop: sub-resources from the
		// same parent need a stable ordering in generated output.
		subNames := make([]string, 0, len(resource.SubResources))
		for sn := range resource.SubResources {
			subNames = append(subNames, sn)
		}
		sort.Strings(subNames)
		for _, subName := range subNames {
			// effectiveName is sharded only when the leaf collides; bare
			// otherwise keeps existing CLIs byte-identical.
			effectiveName := subName
			if subResourceShards.IsSharded(subName) {
				effectiveName = spec.ShardedSubResourceTableName(name, subName)
			}
			subTable := buildSubResourceTable(effectiveName, table.Name)
			tables = append(tables, subTable)
		}
	}

	// Defensive dedup. Sharding handles the common collisions, but a spec
	// author naming a top-level resource the same thing the shard synthesizes
	// (e.g. top-level "gists_commits" plus a multi-parent "commits" under
	// "gists") would otherwise emit two CREATE TABLE statements and two
	// duplicate Upsert<X>Tx methods, breaking the build on regen. Top-level
	// tables are appended before sub-resource tables, so on a Name collision
	// the kept entry carries the top-level Resource (raw spec key) rather
	// than the sub-resource's snake-cased form.
	seen := make(map[string]bool)
	deduped := make([]TableDef, 0, len(tables))
	for _, t := range tables {
		if !seen[t.Name] {
			seen[t.Name] = true
			deduped = append(deduped, t)
		}
	}
	tables = deduped

	tables = append(tables, TableDef{
		Name:     "sync_state",
		Resource: "sync_state",
		Columns: []ColumnDef{
			{Name: "resource_type", Type: "TEXT", PrimaryKey: true},
			{Name: "last_cursor", Type: "TEXT"},
			{Name: "last_synced_at", Type: "DATETIME"},
			{Name: "total_count", Type: "INTEGER DEFAULT 0"},
		},
	})

	return tables
}

// computeDataGravity scores 0-12 based on endpoint count, response field
// count, text fields, temporal fields, and FK references. Caller passes
// the resolved response fields (collectResponseFields) so the function
// doesn't re-walk the spec; gravity scoring uses the same response shape
// the rest of BuildSchema relies on.
func computeDataGravity(r spec.Resource, fields []spec.TypeField) int {
	score := 0

	epCount := len(r.Endpoints)
	if epCount >= 4 {
		score += 4
	} else {
		score += epCount
	}

	if len(fields) >= 10 {
		score += 2
	} else if len(fields) >= 5 {
		score += 1
	}

	textFields := collectTextFieldNamesFromFields(fields)
	if len(textFields) >= 3 {
		score += 2
	} else if len(textFields) >= 1 {
		score += 1
	}

	temporalCount := 0
	fkCount := 0
	for _, f := range fields {
		lower := strings.ToLower(f.Name)
		if strings.HasSuffix(lower, "_at") || strings.Contains(lower, "date") || f.Format == "date-time" {
			temporalCount++
		}
		if strings.HasSuffix(lower, "_id") {
			fkCount++
		}
	}
	if temporalCount >= 2 {
		score += 2
	} else if temporalCount >= 1 {
		score += 1
	}
	if fkCount >= 2 {
		score += 2
	} else if fkCount >= 1 {
		score += 1
	}

	if score > 12 {
		score = 12
	}
	return score
}

// collectResponseFields gathers per-item response fields for a resource's
// endpoints, resolved via s.Types[endpoint.Response.Item]. Fields are
// returned in the order they appear in the response type, deduplicated
// across endpoints (list and detail share an item shape).
//
// Why response Types and not request Params/Body: query/path/body fields
// are inputs sent to the API; sync stores what the API returns. Sourcing
// columns from request-side fields means sync can never populate them.
// When the response type is not registered in s.Types the resource yields
// no fields and the caller leaves the table at id/data/synced_at —
// falling back to request fields would re-emit columns sync can't fill.
//
// GET endpoints are walked first; non-GET (POST/PUT/PATCH) endpoints are
// walked only if no GET endpoint contributed any fields. This handles
// write-only resources (event-emit, webhook ingestion) whose POST/PUT
// response is the canonical record shape, without letting wrapper-shaped
// create-responses pollute typed columns when a GET endpoint exists.
//
// On dedup, an entry without a Format hint is upgraded if a later
// endpoint declares the same field with a non-empty Format. Otherwise
// list-vs-detail Format drift could downgrade a DATETIME column to TEXT
// based on alphabetic endpoint-key order.
func collectResponseFields(s *spec.APISpec, r spec.Resource) []spec.TypeField {
	endpointKeys := make([]string, 0, len(r.Endpoints))
	for k := range r.Endpoints {
		endpointKeys = append(endpointKeys, k)
	}
	sort.Strings(endpointKeys)

	collect := func(predicate func(method string) bool) []spec.TypeField {
		seenIdx := make(map[string]int)
		var fields []spec.TypeField
		for _, key := range endpointKeys {
			ep := r.Endpoints[key]
			if !predicate(ep.Method) {
				continue
			}
			typeName := ep.Response.Item
			if typeName == "" {
				continue
			}
			typeDef, ok := s.Types[typeName]
			if !ok {
				continue
			}
			for _, f := range typeDef.Fields {
				if existing, ok := seenIdx[f.Name]; ok {
					if fields[existing].Format == "" && f.Format != "" {
						fields[existing].Format = f.Format
					}
					continue
				}
				seenIdx[f.Name] = len(fields)
				fields = append(fields, f)
			}
		}
		return fields
	}

	if got := collect(func(m string) bool { return m == "GET" }); len(got) > 0 {
		return got
	}
	return collect(func(m string) bool { return m != "GET" })
}

// isScalarTypeField returns true for string/int/bool/number TypeFields
// (not objects/arrays).
func isScalarTypeField(f spec.TypeField) bool {
	switch strings.ToLower(f.Type) {
	case "string", "integer", "int", "boolean", "bool", "number", "float":
		return true
	default:
		return false
	}
}

// sqliteType maps spec types to SQLite column types.
func sqliteType(goType, format string) string {
	switch strings.ToLower(goType) {
	case "integer", "int":
		return "INTEGER"
	case "number", "float":
		return "REAL"
	case "boolean", "bool":
		return "INTEGER"
	case "string":
		if format == "date-time" || format == "date" {
			return "DATETIME"
		}
		return "TEXT"
	default:
		return "TEXT"
	}
}

// textFieldKeywords flags response field names that should feed the FTS5
// index. Defined at package scope so callers don't reallocate per call.
var textFieldKeywords = map[string]bool{
	"title": true, "name": true, "description": true,
	"body": true, "content": true, "summary": true, "subject": true,
	"text": true, "message": true, "comment": true, "note": true,
	"notes": true, "tag": true, "tags": true, "label": true, "labels": true,
	"category": true, "categories": true, "metadata": true,
}

// collectTextFieldNamesFromFields picks searchable scalar string fields
// out of an already-resolved response field list. Keeps the FTS index
// pinned to fields sync actually stores — request-side filter knobs like
// `tags` or `labels` never appear here because they aren't in the
// response schema.
func collectTextFieldNamesFromFields(fields []spec.TypeField) []string {
	seen := make(map[string]bool)
	var result []string
	for _, f := range fields {
		lower := strings.ToLower(f.Name)
		if !textFieldKeywords[lower] || seen[lower] || !isScalarTypeField(f) {
			continue
		}
		seen[lower] = true
		result = append(result, toSnakeCase(f.Name))
	}
	return result
}

// hasTypeField reports whether fields contains an entry whose name matches
// the given snake_cased-or-lowered key.
func hasTypeField(fields []spec.TypeField, name string) bool {
	for _, f := range fields {
		if toSnakeCase(f.Name) == name || strings.ToLower(f.Name) == name {
			return true
		}
	}
	return false
}

func buildTable(s *spec.APISpec, name string, r spec.Resource) TableDef {
	tableName := toSnakeCase(name)
	fields := collectResponseFields(s, r)
	gravity := computeDataGravity(r, fields)

	columns := append([]ColumnDef(nil), baseTableColumns...)
	table := TableDef{
		Name:     tableName,
		Resource: name,
		Columns:  columns,
	}

	if gravity >= 2 {
		seenColumns := map[string]bool{}
		for _, c := range columns {
			seenColumns[c.Name] = true
		}
		seenIndexes := map[string]bool{}
		for _, f := range fields {
			colName := toSnakeCase(f.Name)
			if isScalarTypeField(f) && !seenColumns[colName] {
				seenColumns[colName] = true
				table.Columns = append(table.Columns, ColumnDef{
					Name: colName,
					Type: sqliteType(f.Type, f.Format),
				})
			}
			if strings.HasSuffix(strings.ToLower(f.Name), "_id") {
				idxName := "idx_" + tableName + "_" + colName
				if !seenIndexes[idxName] {
					seenIndexes[idxName] = true
					table.Indexes = append(table.Indexes, IndexDef{
						Name:      idxName,
						TableName: tableName,
						Columns:   colName,
					})
				}
			}
		}
		for _, temporal := range []string{"created_at", "updated_at"} {
			if hasTypeField(fields, temporal) {
				table.Indexes = append(table.Indexes, IndexDef{
					Name:      "idx_" + tableName + "_" + temporal,
					TableName: tableName,
					Columns:   temporal,
				})
			}
		}
	}

	textFields := collectTextFieldNamesFromFields(fields)
	if len(textFields) >= 2 && gravity >= 2 {
		table.FTS5 = true
		table.FTS5Fields = textFields
		table.FTS5Triggers = true
	}

	return table
}

// buildSubResourceTable creates a fixed 4-column table for a sub-resource.
// Sub-resources always use a simplified schema (id, parent_id, data, synced_at)
// to avoid complex schema-drift migrations on update; they do not gain
// response-derived typed columns or FTS5 indices.
func buildSubResourceTable(name string, parentTable string) TableDef {
	tableName := toSnakeCase(name)
	parentCol := parentTable + "_id"

	columns := []ColumnDef{
		baseTableColumns[0], // id
		{Name: parentCol, Type: "TEXT", NotNull: true},
		baseTableColumns[1], // data
		baseTableColumns[2], // synced_at
	}

	return TableDef{
		Name:     tableName,
		Resource: name,
		Columns:  columns,
		Indexes: []IndexDef{
			{
				Name:      "idx_" + tableName + "_" + parentCol,
				TableName: tableName,
				Columns:   parentCol,
			},
		},
	}
}

// safeSQLName returns an identifier that is safe to use in SQLite DDL.
// Always double-quotes the name, escaping any embedded quote. Quoting is
// harmless for non-keyword identifiers and is the only way to safely emit
// SQLite strict-reserved keywords like "add", "to", and "from" in CREATE
// TABLE / CREATE INDEX context, where they otherwise fail at parse time.
// Maintaining a hand-rolled keyword allowlist diverges from SQLite over
// time; quoting unconditionally is the durable contract.
func safeSQLName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func sqlStringLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

// toSnakeCase aliases spec.ToSnakeCase; shared so profiler/schema agree.
func toSnakeCase(s string) string {
	return spec.ToSnakeCase(s)
}
