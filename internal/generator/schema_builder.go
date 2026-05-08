package generator

import (
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

type TableDef struct {
	Name         string
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
		fields := collectResponseFields(s, resource)
		gravity := computeDataGravity(resource, fields)
		tableName := toSnakeCase(name)

		table := TableDef{
			Name:    tableName,
			Columns: append([]ColumnDef(nil), baseTableColumns...),
		}

		if gravity >= 2 {
			seenColumns := map[string]bool{}
			for _, c := range baseTableColumns {
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
			// Only use content-sync triggers when ALL FTS fields are
			// actual extracted columns on the table. Otherwise the
			// triggers reference non-existent columns and fail.
			table.FTS5Triggers = true
		}

		tables = append(tables, table)

		// Same determinism concern as the outer loop: sub-resources from the
		// same parent need a stable ordering in generated output.
		subNames := make([]string, 0, len(resource.SubResources))
		for sn := range resource.SubResources {
			subNames = append(subNames, sn)
		}
		sort.Strings(subNames)
		for _, subName := range subNames {
			subResource := resource.SubResources[subName]
			// effectiveName is sharded only when the leaf collides; bare
			// otherwise keeps existing CLIs byte-identical.
			effectiveName := subName
			if subResourceShards.IsSharded(subName) {
				effectiveName = spec.ShardedSubResourceTableName(name, subName)
			}
			subTable := buildSubResourceTable(effectiveName, subResource, tableName)
			tables = append(tables, subTable)
		}
	}

	// Defensive dedup. Sharding handles the common collisions, but a spec
	// author naming a top-level resource the same thing the shard synthesizes
	// (e.g. top-level "gists_commits" plus a multi-parent "commits" under
	// "gists") would otherwise emit two CREATE TABLE statements and two
	// duplicate Upsert<X>Tx methods, breaking the build on regen.
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
		Name: "sync_state",
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

// buildSubResourceTable creates a table definition for a sub-resource with
// a foreign key column referencing the parent table. Sub-resources share
// the base id/data/synced_at shape with top-level tables and add a
// parent_id column between id and data.
func buildSubResourceTable(name string, r spec.Resource, parentTable string) TableDef {
	tableName := toSnakeCase(name)
	parentCol := parentTable + "_id"

	columns := make([]ColumnDef, 0, len(baseTableColumns)+1)
	columns = append(columns, baseTableColumns[0]) // id
	columns = append(columns, ColumnDef{Name: parentCol, Type: "TEXT", NotNull: true})
	columns = append(columns, baseTableColumns[1:]...) // data, synced_at

	return TableDef{
		Name:    tableName,
		Columns: columns,
		Indexes: []IndexDef{
			{
				Name:      "idx_" + tableName + "_" + parentCol,
				TableName: tableName,
				Columns:   parentCol,
			},
		},
	}
}

// sqlReservedWords is the set of SQL keywords that must be quoted when used
// as table or column names. Covers SQLite reserved words plus common SQL
// keywords that appear as API resource or field names.
var sqlReservedWords = map[string]bool{
	"check": true, "default": true, "from": true, "to": true,
	"order": true, "group": true, "select": true, "where": true,
	"table": true, "column": true, "index": true, "key": true,
	"values": true, "references": true, "create": true, "drop": true,
	"insert": true, "update": true, "delete": true, "set": true,
	"join": true, "on": true, "in": true, "not": true, "null": true,
	"primary": true, "foreign": true, "unique": true, "like": true,
	"between": true, "exists": true, "having": true, "limit": true,
	"offset": true, "union": true, "except": true, "case": true,
	"when": true, "then": true, "else": true, "end": true,
	"as": true, "is": true, "by": true, "and": true, "or": true,
	"transaction": true, "begin": true, "commit": true, "rollback": true,
	"trigger": true, "view": true, "replace": true, "match": true,
}

// safeSQLName returns an identifier that is safe to use in SQLite DDL.
// SQLite accepts many names when double-quoted, so quote reserved words and
// anything that is not a valid bare identifier (for example, names beginning
// with a digit).
func safeSQLName(name string) string {
	if sqlReservedWords[strings.ToLower(name)] || !isBareSQLiteIdentifier(name) {
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
	return name
}

func isBareSQLiteIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
			continue
		}
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func sqlStringLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

// toSnakeCase aliases spec.ToSnakeCase; shared so profiler/schema agree.
func toSnakeCase(s string) string {
	return spec.ToSnakeCase(s)
}
