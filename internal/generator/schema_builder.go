package generator

import (
	"sort"
	"strings"
	"unicode"

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

	for _, name := range resourceNames {
		resource := s.Resources[name]
		gravity := computeDataGravity(name, resource)
		tableName := toSnakeCase(name)

		table := TableDef{
			Name: tableName,
			Columns: []ColumnDef{
				{Name: "id", Type: "TEXT", PrimaryKey: true},
				{Name: "data", Type: "JSON", NotNull: true},
				{Name: "synced_at", Type: "DATETIME DEFAULT CURRENT_TIMESTAMP"},
			},
		}

		if gravity >= 2 {
			fields := collectResponseFields(resource)
			for _, f := range fields {
				if isScalarField(f) && f.Name != "id" {
					col := ColumnDef{
						Name: toSnakeCase(f.Name),
						Type: sqliteType(f.Type, f.Format),
					}
					table.Columns = append(table.Columns, col)
				}
				if strings.HasSuffix(strings.ToLower(f.Name), "_id") {
					table.Indexes = append(table.Indexes, IndexDef{
						Name:      "idx_" + tableName + "_" + toSnakeCase(f.Name),
						TableName: tableName,
						Columns:   toSnakeCase(f.Name),
					})
				}
			}
			for _, temporal := range []string{"created_at", "updated_at"} {
				if hasField(fields, temporal) {
					table.Indexes = append(table.Indexes, IndexDef{
						Name:      "idx_" + tableName + "_" + temporal,
						TableName: tableName,
						Columns:   temporal,
					})
				}
			}
		}

		textFields := collectTextFieldNames(resource)
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
			subTable := buildSubResourceTable(subName, subResource, tableName)
			tables = append(tables, subTable)
		}
	}

	// Deduplicate tables by name (sub-resources from different parents can collide)
	seen := make(map[string]bool)
	var deduped []TableDef
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

// computeDataGravity scores 0-12 based on endpoint count, field count,
// text fields, temporal fields, and FK references.
func computeDataGravity(name string, r spec.Resource) int {
	score := 0

	// Endpoint count: 1pt per endpoint, max 4
	epCount := len(r.Endpoints)
	if epCount >= 4 {
		score += 4
	} else {
		score += epCount
	}

	// Field count from all params/body across endpoints
	totalFields := 0
	for _, ep := range r.Endpoints {
		totalFields += len(ep.Params) + len(ep.Body)
	}
	if totalFields >= 10 {
		score += 2
	} else if totalFields >= 5 {
		score += 1
	}

	// Text fields bonus
	textFields := collectTextFieldNames(r)
	if len(textFields) >= 3 {
		score += 2
	} else if len(textFields) >= 1 {
		score += 1
	}

	// Temporal fields bonus
	allFields := collectResponseFields(r)
	temporalCount := 0
	for _, f := range allFields {
		lower := strings.ToLower(f.Name)
		if strings.HasSuffix(lower, "_at") || strings.Contains(lower, "date") || f.Format == "date-time" {
			temporalCount++
		}
	}
	if temporalCount >= 2 {
		score += 2
	} else if temporalCount >= 1 {
		score += 1
	}

	// FK references bonus
	fkCount := 0
	for _, f := range allFields {
		if strings.HasSuffix(strings.ToLower(f.Name), "_id") {
			fkCount++
		}
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

// collectResponseFields gathers all field specs from GET endpoints.
func collectResponseFields(r spec.Resource) []spec.Param {
	seen := make(map[string]bool)
	var fields []spec.Param

	for _, ep := range r.Endpoints {
		if ep.Method != "GET" {
			continue
		}
		for _, p := range ep.Params {
			if !seen[p.Name] {
				seen[p.Name] = true
				fields = append(fields, p)
			}
		}
		for _, p := range ep.Body {
			if !seen[p.Name] {
				seen[p.Name] = true
				fields = append(fields, p)
			}
		}
	}

	// Also include body fields from POST/PUT as they often mirror response shape
	for _, ep := range r.Endpoints {
		if ep.Method == "GET" {
			continue
		}
		for _, p := range ep.Body {
			if !seen[p.Name] {
				seen[p.Name] = true
				fields = append(fields, p)
			}
		}
	}

	return fields
}

// isScalarField returns true for string/int/bool/number fields (not objects/arrays).
func isScalarField(p spec.Param) bool {
	switch strings.ToLower(p.Type) {
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

// collectTextFieldNames finds fields likely to contain searchable text.
func collectTextFieldNames(r spec.Resource) []string {
	textKeywords := map[string]bool{
		"title": true, "name": true, "description": true,
		"body": true, "content": true, "summary": true, "subject": true,
		"text": true, "message": true, "comment": true, "note": true,
		"notes": true, "tag": true, "tags": true, "label": true, "labels": true,
		"category": true, "categories": true, "metadata": true,
	}

	seen := make(map[string]bool)
	var result []string

	for _, ep := range r.Endpoints {
		allParams := append(ep.Params, ep.Body...)
		for _, p := range allParams {
			lower := strings.ToLower(p.Name)
			if textKeywords[lower] && !seen[lower] && isScalarField(p) {
				seen[lower] = true
				result = append(result, toSnakeCase(p.Name))
			}
		}
	}

	return result
}

// hasField checks if a field with the given name exists in the list.
func hasField(fields []spec.Param, name string) bool {
	for _, f := range fields {
		if toSnakeCase(f.Name) == name || strings.ToLower(f.Name) == name {
			return true
		}
	}
	return false
}

// buildSubResourceTable creates a table definition for a sub-resource with
// a foreign key column referencing the parent table.
func buildSubResourceTable(name string, r spec.Resource, parentTable string) TableDef {
	tableName := toSnakeCase(name)

	table := TableDef{
		Name: tableName,
		Columns: []ColumnDef{
			{Name: "id", Type: "TEXT", PrimaryKey: true},
			{Name: parentTable + "_id", Type: "TEXT", NotNull: true},
			{Name: "data", Type: "JSON", NotNull: true},
			{Name: "synced_at", Type: "DATETIME DEFAULT CURRENT_TIMESTAMP"},
		},
		Indexes: []IndexDef{
			{
				Name:      "idx_" + tableName + "_" + parentTable + "_id",
				TableName: tableName,
				Columns:   parentTable + "_id",
			},
		},
	}

	return table
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

// toSnakeCase converts camelCase, PascalCase, or kebab-case to snake_case.
// Expects ASCII input — callers are SQL identifier paths whose inputs come
// from parsed APISpec types/fields that already passed through the
// openapi parser's ASCIIFold chokepoints.
func toSnakeCase(s string) string {
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.ReplaceAll(s, "-", "_")

	var result strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			prev := rune(s[i-1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				result.WriteRune('_')
			} else if unicode.IsUpper(prev) && i+1 < len(s) && unicode.IsLower(rune(s[i+1])) {
				result.WriteRune('_')
			}
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
}
