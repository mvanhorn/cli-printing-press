package generator

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// synthesizedRunnableExample builds a help Example. Operations that already
// have parameter examples, or required parameters the example must include,
// keep the parameter synthesis path. Body-only operations use the request
// body instead. pp:happy-args is not derived here.
func (g *Generator) synthesizedRunnableExample(commandParts []string, endpoint spec.Endpoint) string {
	if !endpointExampleFollowsParameters(endpoint) {
		if parts, ok := requestBodyExampleArgParts(endpoint); ok {
			line := append([]string{naming.CLI(g.Spec.Name)}, commandParts...)
			line = append(line, parts...)
			return runnableExampleLine("  " + shellargs.Join(line))
		}
	}
	if !requiredInputsAreDerivable(endpoint) {
		return ""
	}
	parts := []string{naming.CLI(g.Spec.Name)}
	parts = append(parts, commandParts...)
	parts = append(parts, commandExampleArgParts(endpoint)...)
	return runnableExampleLine("  " + strings.Join(parts, " "))
}

func endpointExampleFollowsParameters(ep spec.Endpoint) bool {
	for _, p := range ep.Params {
		if p.Required {
			return true
		}
		if _, ok := exampleArgString(p.Example); ok {
			return true
		}
	}
	return false
}

func requestBodyExampleArgParts(ep spec.Endpoint) ([]string, bool) {
	flat := endpointUsesMultipart(ep) || endpointUsesForm(ep)
	if ep.BodyJSONFallback {
		text, ok := exampleArgString(ep.RequestBodyExample)
		if !ok {
			return nil, false
		}
		return []string{"--body-json", text}, true
	}
	if obj, ok := exampleObject(ep.RequestBodyExample); ok {
		if parts := mediaBodyExampleFlags(ep.Body, obj, "", 0, flat); len(parts) > 0 {
			return parts, true
		}
	}
	parts := requiredBodyExampleFlags(ep.Body, "", 0, flat)
	if len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

func mediaBodyExampleFlags(body []spec.Param, obj map[string]any, prefix string, depth int, flat bool) []string {
	var parts []string
	for _, p := range body {
		val, ok := exampleField(obj, p)
		if !ok {
			continue
		}
		parts = append(parts, emitBodyExampleValue(p, val, prefix, depth, flat)...)
	}
	return parts
}

func requiredBodyExampleFlags(body []spec.Param, prefix string, depth int, flat bool) []string {
	var parts []string
	for _, p := range body {
		if !p.Required {
			continue
		}
		if !flat && p.Type == "object" && len(p.Fields) > 0 && depth+1 < maxBodyFlagDepth {
			flag := joinFlag(prefix, publicFlagName(p))
			if child, ok := exampleObject(p.Example); ok {
				parts = append(parts, mediaBodyExampleFlags(p.Fields, child, flag, depth+1, flat)...)
				continue
			}
			parts = append(parts, requiredBodyExampleFlags(p.Fields, flag, depth+1, flat)...)
			continue
		}
		text, ok := exampleArgString(p.Example)
		if !ok {
			continue
		}
		parts = appendExampleFlag(parts, joinFlag(prefix, publicFlagName(p)), text)
	}
	return parts
}

func emitBodyExampleValue(p spec.Param, val any, prefix string, depth int, flat bool) []string {
	flag := joinFlag(prefix, publicFlagName(p))
	if !flat && p.Type == "object" && len(p.Fields) > 0 && depth+1 < maxBodyFlagDepth {
		child, ok := exampleObject(val)
		if !ok {
			return nil
		}
		return mediaBodyExampleFlags(p.Fields, child, flag, depth+1, flat)
	}
	text, ok := exampleArgString(val)
	if !ok {
		return nil
	}
	return appendExampleFlag(nil, flag, text)
}

func exampleField(obj map[string]any, p spec.Param) (any, bool) {
	if obj == nil {
		return nil, false
	}
	if v, ok := obj[p.BodyWireName()]; ok && v != nil {
		return v, true
	}
	if name := p.Name; name != "" && name != p.BodyWireName() {
		if v, ok := obj[name]; ok && v != nil {
			return v, true
		}
	}
	return nil, false
}

func appendExampleFlag(parts []string, flag, value string) []string {
	if flag == "" || value == "" {
		return parts
	}
	return append(parts, "--"+flag, value)
}

func exampleObject(v any) (map[string]any, bool) {
	obj, ok := v.(map[string]any)
	if !ok || len(obj) == 0 {
		return nil, false
	}
	return obj, true
}

func exampleArgString(v any) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		if strings.TrimSpace(t) == "" {
			return "", false
		}
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case json.Number:
		s := strings.TrimSpace(t.String())
		if s == "" {
			return "", false
		}
		return s, true
	case float32:
		return formatExampleFloat(float64(t))
	case float64:
		return formatExampleFloat(t)
	default:
		return formatExampleJSON(t)
	}
}

func formatExampleJSON(v any) (string, bool) {
	b, err := json.Marshal(v)
	if err != nil || len(b) == 0 || string(b) == "null" {
		return "", false
	}
	return string(b), true
}

func formatExampleFloat(v float64) (string, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "", false
	}
	if v == math.Trunc(v) && v >= math.MinInt64 && v <= math.MaxInt64 {
		return strconv.FormatInt(int64(v), 10), true
	}
	return strconv.FormatFloat(v, 'f', -1, 64), true
}
