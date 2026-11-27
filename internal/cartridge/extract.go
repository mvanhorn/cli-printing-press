package cartridge

import (
	"fmt"
	"strconv"
	"strings"
)

// ExtractPath evaluates a lightweight JSONPath expression against decoded JSON
// values. Supported forms: "$", "$.a.b", "$.items[*].name", "$.items[0].id",
// and bare dot paths ("a.b"). Array wildcards flatten nested slices cleanly.
func ExtractPath(root any, expr string) any {
	path := strings.TrimSpace(expr)
	if path == "" || path == "$" {
		return root
	}
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")
	segments := splitPath(path)
	return evalSegments(root, segments)
}

func splitPath(path string) []string {
	var out []string
	var cur strings.Builder
	for i := range len(path) {
		ch := path[i]
		switch ch {
		case '.':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		case '[':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			cur.WriteByte(ch)
		case ']':
			cur.WriteByte(ch)
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func evalSegments(cur any, segments []string) any {
	if cur == nil || len(segments) == 0 {
		return cur
	}
	seg := segments[0]
	rest := segments[1:]

	if seg == "[*]" {
		slice, ok := cur.([]any)
		if !ok {
			return nil
		}
		if len(rest) == 0 {
			return slice
		}
		var collected []any
		for _, item := range slice {
			val := evalSegments(item, rest)
			if val == nil {
				continue
			}
			if subSlice, isSlice := val.([]any); isSlice {
				collected = append(collected, subSlice...)
			} else {
				collected = append(collected, val)
			}
		}
		return collected
	}

	if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
		idxStr := strings.TrimSuffix(strings.TrimPrefix(seg, "["), "]")
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			return nil
		}
		slice, ok := cur.([]any)
		if !ok || idx < 0 || idx >= len(slice) {
			return nil
		}
		return evalSegments(slice[idx], rest)
	}

	switch typed := cur.(type) {
	case map[string]any:
		next, ok := typed[seg]
		if !ok {
			return nil
		}
		return evalSegments(next, rest)
	case []any:
		var collected []any
		for _, item := range typed {
			val := evalSegments(item, segments)
			if val != nil {
				collected = append(collected, val)
			}
		}
		return collected
	default:
		return nil
	}
}

// ProjectFields narrows an object or slice of objects to only the requested
// dot-path fields, cutting agent token spend by 60-80% in --compact mode.
func ProjectFields(data any, fields []string) any {
	if len(fields) == 0 || data == nil {
		return data
	}
	switch v := data.(type) {
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, projectObject(item, fields))
		}
		return out
	case map[string]any:
		return projectObject(v, fields)
	default:
		return data
	}
}

func projectObject(item any, fields []string) any {
	m, ok := item.(map[string]any)
	if !ok {
		return item
	}
	projected := make(map[string]any, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		key := f
		expr := f
		if alias, target, hasAlias := strings.Cut(f, "="); hasAlias {
			key = strings.TrimSpace(alias)
			expr = strings.TrimSpace(target)
		}
		val := ExtractPath(m, expr)
		if val != nil {
			projected[key] = val
		}
	}
	return projected
}

// StringifyField extracts a single dot-path value as a concise string.
func StringifyField(item any, expr string) string {
	val := ExtractPath(item, expr)
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(v)
	}
}
