package browsersniff

import (
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

const maxSchemaDepth = 3

var (
	uuidPattern  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

type inferredField struct {
	count  int
	param  spec.Param
	nested map[string]*inferredField
}

func InferResponseSchema(bodies []string) []spec.Param {
	parsedSamples := make([]map[string]any, 0, len(bodies))
	for _, body := range bodies {
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}

		var value any
		if err := json.Unmarshal([]byte(body), &value); err != nil {
			continue
		}

		root := topLevelObject(value)
		if root == nil {
			continue
		}

		parsedSamples = append(parsedSamples, root)
	}

	if len(parsedSamples) == 0 {
		return nil
	}

	fields := make(map[string]*inferredField)
	for _, sample := range parsedSamples {
		mergeObject(fields, sample, 1)
	}

	return buildParams(fields, len(parsedSamples))
}

func InferRequestSchema(body string, contentType string) []spec.Param {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}

	contentType = strings.ToLower(contentType)
	switch {
	case strings.Contains(contentType, "json"):
		var value any
		if err := json.Unmarshal([]byte(body), &value); err != nil {
			return nil
		}

		root := topLevelObject(value)
		if root == nil {
			return nil
		}

		fields := make(map[string]*inferredField)
		mergeObject(fields, root, 1)
		return buildParams(fields, 1)
	case strings.Contains(contentType, "form-urlencoded"):
		values := ParseFormBody(body)
		if len(values) == 0 {
			return nil
		}

		params := make([]spec.Param, 0, len(values))
		for key, value := range values {
			params = append(params, spec.Param{
				Name:        key,
				Type:        inferScalarStringType(value),
				Required:    true,
				Description: "",
			})
		}

		sort.Slice(params, func(i, j int) bool {
			return params[i].Name < params[j].Name
		})
		return params
	default:
		return nil
	}
}

func ParseFormBody(body string) map[string]string {
	values, err := url.ParseQuery(body)
	if err != nil {
		return map[string]string{}
	}

	parsed := make(map[string]string, len(values))
	for key, vals := range values {
		if len(vals) == 0 {
			parsed[key] = ""
			continue
		}
		parsed[key] = vals[0]
	}

	return parsed
}

func topLevelObject(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case []any:
		if len(typed) == 0 {
			return nil
		}

		object, ok := typed[0].(map[string]any)
		if !ok {
			return nil
		}
		return object
	default:
		return nil
	}
}

func mergeObject(fields map[string]*inferredField, object map[string]any, depth int) {
	for name, value := range object {
		if value == nil {
			continue
		}

		field, ok := fields[name]
		if !ok {
			field = &inferredField{}
			fields[name] = field
		}

		field.count++
		field.param = inferParam(name, value, depth)

		if field.param.Type == "object" && len(field.param.Fields) > 0 {
			if field.nested == nil {
				field.nested = make(map[string]*inferredField)
			}

			child, ok := value.(map[string]any)
			if ok {
				mergeObject(field.nested, child, depth+1)
			}
		}
	}
}

func inferParam(name string, value any, depth int) spec.Param {
	param := spec.Param{
		Name:        name,
		Description: "",
	}

	switch typed := value.(type) {
	case float64:
		if math.Trunc(typed) == typed {
			param.Type = "integer"
		} else {
			param.Type = "number"
		}
	case string:
		param.Type = "string"
		param.Format = inferStringFormat(typed)
	case bool:
		param.Type = "boolean"
	case []any:
		param.Type = "array"
		if len(typed) > 0 {
			if firstObject, ok := typed[0].(map[string]any); ok && depth < maxSchemaDepth {
				param.Fields = inferObjectFields(firstObject, depth+1)
			}
		}
	case map[string]any:
		param.Type = "object"
		if depth < maxSchemaDepth {
			param.Fields = inferObjectFields(typed, depth+1)
		}
	default:
		param.Type = "string"
	}

	return param
}

func inferObjectFields(object map[string]any, depth int) []spec.Param {
	if depth > maxSchemaDepth {
		return nil
	}

	params := make([]spec.Param, 0, len(object))
	for name, value := range object {
		if value == nil {
			continue
		}

		child := inferParam(name, value, depth)
		child.Required = true
		params = append(params, child)
	}

	sort.Slice(params, func(i, j int) bool {
		return params[i].Name < params[j].Name
	})
	return params
}

func buildParams(fields map[string]*inferredField, sampleCount int) []spec.Param {
	params := make([]spec.Param, 0, len(fields))
	for name, field := range fields {
		param := field.param
		param.Name = name
		param.Required = field.count == sampleCount
		if field.param.Type == "object" && field.nested != nil {
			param.Fields = buildParams(field.nested, field.count)
		}
		params = append(params, param)
	}

	sort.Slice(params, func(i, j int) bool {
		return params[i].Name < params[j].Name
	})
	return params
}

func inferStringFormat(value string) string {
	switch {
	case isRFC3339(value):
		return "date-time"
	case uuidPattern.MatchString(value):
		return "uuid"
	case emailPattern.MatchString(value):
		return "email"
	case isURL(value):
		return "url"
	default:
		return ""
	}
}

func isRFC3339(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

func isURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return parsed.Scheme != "" && parsed.Host != ""
}

func inferScalarStringType(value string) string {
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return "integer"
	}

	if value == "true" || value == "false" {
		return "boolean"
	}

	return "string"
}
