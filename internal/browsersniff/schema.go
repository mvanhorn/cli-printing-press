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

// containerKind records whether every observed value of a field was the same
// empty container. containerUntracked means this field was copied from an
// already-inferred param (the multi-sample request merge) rather than walked
// as raw JSON.
type containerKind int

const (
	containerUntracked containerKind = iota
	containerEmptyArray
	containerEmptyObject
	containerValue
)

type inferredField struct {
	count     int
	param     spec.Param
	nested    map[string]*inferredField
	container containerKind
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

	// Payload syntax wins over the advertised media type. A valid JSON object
	// sent as application/x-www-form-urlencoded is still JSON fields, not one
	// form-field name equal to the raw document.
	if root, ok := parseJSONRequestObject(body); ok {
		fields := make(map[string]*inferredField)
		mergeObject(fields, root, 1)
		return buildParams(fields, 1)
	}

	if strings.Contains(strings.ToLower(contentType), "form-urlencoded") {
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
				Default:     value,
				Description: "",
			})
		}

		sort.Slice(params, func(i, j int) bool {
			return params[i].Name < params[j].Name
		})
		return params
	}

	return nil
}

func parseJSONRequestObject(body string) (map[string]any, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, false
	}
	var value any
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		return nil, false
	}
	root := topLevelObject(value)
	if root == nil {
		return nil, false
	}
	return root, true
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
		field.observe(value)
		field.param = inferParam(name, value, depth)

		if (field.param.Type == "object" || field.param.Type == "array") && len(field.param.Fields) > 0 {
			if field.nested == nil {
				field.nested = make(map[string]*inferredField)
			}

			if child, ok := nestedObjectSample(value); ok {
				mergeObject(field.nested, child, depth+1)
			}
		}
	}
}

func nestedObjectSample(value any) (map[string]any, bool) {
	if child, ok := value.(map[string]any); ok {
		return child, true
	}
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil, false
	}
	child, ok := items[0].(map[string]any)
	return child, ok
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
		if kind := classifyContainer(value); kind == containerEmptyArray || kind == containerEmptyObject {
			// One empty container is not evidence the caller must supply the field.
			// buildParams repeats the check across every sample.
			child.Required = false
			child.Default = emptyContainerValue(kind)
		} else {
			child.Required = true
		}
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
		if (field.param.Type == "object" || field.param.Type == "array") && field.nested != nil && field.observedEmptyKind() == containerUntracked {
			param.Fields = buildParams(field.nested, field.count)
		}
		// Present in every sample is not enough when every value was [].
		// The generated flag stays optional and the body keeps that shape.
		if kind := field.observedEmptyKind(); kind != containerUntracked {
			param.Required = false
			param.Default = emptyContainerValue(kind)
			param.Fields = nil
		} else if kind := stampedEmptyContainerKind(param); kind != containerUntracked {
			param.Required = false
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

func (f *inferredField) observe(value any) {
	kind := classifyContainer(value)
	if f.container == containerUntracked {
		f.container = kind
		return
	}
	if f.container != kind {
		f.container = containerValue
	}
}

func (f *inferredField) observedEmptyKind() containerKind {
	switch f.container {
	case containerEmptyArray, containerEmptyObject:
		return f.container
	default:
		return containerUntracked
	}
}

func classifyContainer(value any) containerKind {
	switch typed := value.(type) {
	case []any:
		if len(typed) == 0 {
			return containerEmptyArray
		}
	case map[string]any:
		if len(typed) == 0 {
			return containerEmptyObject
		}
	}
	return containerValue
}

func emptyContainerValue(kind containerKind) any {
	switch kind {
	case containerEmptyArray:
		return []any{}
	case containerEmptyObject:
		return map[string]any{}
	default:
		return nil
	}
}

func stampedEmptyContainerKind(param spec.Param) containerKind {
	if param.Default == nil || len(param.Fields) > 0 {
		return containerUntracked
	}
	data, err := json.Marshal(param.Default)
	if err != nil {
		return containerUntracked
	}
	switch string(data) {
	case "[]":
		if param.Type == "array" {
			return containerEmptyArray
		}
	case "{}":
		if param.Type == "object" {
			return containerEmptyObject
		}
	}
	return containerUntracked
}

// mergeObservedRequestParam combines two single-sample inferences of one
// field. An empty container stays optional only when every sample was that
// same empty container. A later [] must not erase an earlier real value.
func mergeObservedRequestParam(prev, next spec.Param, first bool) spec.Param {
	if first {
		return next
	}
	prevKind := stampedEmptyContainerKind(prev)
	nextKind := stampedEmptyContainerKind(next)
	if prevKind != containerUntracked && nextKind != containerUntracked && prevKind != nextKind {
		next.Default = nil
		next.Required = true
		return next
	}
	if prevKind == containerUntracked && nextKind != containerUntracked {
		// An empty object has no fields. Merging them in would drop the real
		// children, but this sample did omit every one of them.
		if nextKind == containerEmptyObject {
			prev.Fields = markFieldsAbsent(prev.Fields)
		}
		return prev
	}
	next.Fields = preserveNonEmptyOverEmpty(prev.Fields, next.Fields)
	return keepNotedAbsence(prev, next)
}

func preserveNonEmptyOverEmpty(prev, next []spec.Param) []spec.Param {
	if len(next) == 0 || len(prev) == 0 {
		return next
	}
	prevByName := make(map[string]spec.Param, len(prev))
	for _, param := range prev {
		prevByName[param.Name] = param
	}
	out := make([]spec.Param, 0, len(next)+len(prev))
	seen := make(map[string]bool, len(next))
	for _, param := range next {
		seen[param.Name] = true
		earlier, ok := prevByName[param.Name]
		if !ok {
			out = append(out, param)
			continue
		}
		earlierKind := stampedEmptyContainerKind(earlier)
		nextKind := stampedEmptyContainerKind(param)
		switch {
		case nextKind != containerUntracked && earlierKind != containerUntracked && nextKind != earlierKind:
			param.Default = nil
			param.Required = true
			param.Fields = nil
			out = append(out, keepNotedAbsence(earlier, param))
		case nextKind != containerUntracked && earlierKind == containerUntracked:
			out = append(out, earlier)
		case nextKind == containerUntracked && earlierKind == containerUntracked:
			param.Fields = preserveNonEmptyOverEmpty(earlier.Fields, param.Fields)
			out = append(out, keepNotedAbsence(earlier, param))
		default:
			if nextKind == containerUntracked && earlierKind != containerUntracked {
				param.Fields = preserveNonEmptyOverEmpty(earlier.Fields, param.Fields)
			}
			out = append(out, keepNotedAbsence(earlier, param))
		}
	}
	// A sample that omits a child must not erase a real value seen earlier.
	// The next sample may show that child as [], and that [] is not evidence
	// the field is an empty default. The omission does disprove required.
	for _, earlier := range prev {
		if seen[earlier.Name] || stampedEmptyContainerKind(earlier) != containerUntracked {
			continue
		}
		out = append(out, markFieldAbsent(earlier))
	}
	return out
}

// A real field left out of a sample stays in the schema so a later [] cannot
// become its default, but that sample is evidence the caller can omit it.
// Children stay required: the generator checks them only when this object is supplied.
func markFieldAbsent(param spec.Param) spec.Param {
	if stampedEmptyContainerKind(param) == containerUntracked {
		param.Required = false
		param.Default = nil
	}
	return param
}

func markFieldsAbsent(fields []spec.Param) []spec.Param {
	out := make([]spec.Param, len(fields))
	for i, param := range fields {
		out[i] = markFieldAbsent(param)
	}
	return out
}

func keepNotedAbsence(earlier, param spec.Param) spec.Param {
	if earlier.Required || stampedEmptyContainerKind(earlier) != containerUntracked {
		return param
	}
	param.Required = false
	param.Default = nil
	return param
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
