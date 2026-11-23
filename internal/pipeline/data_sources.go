package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const maxDataSources = 12

// Unrelated to the research `sources/` directory of downloaded reference repos.
type DataSource struct {
	Name     string `json:"name"`
	Homepage string `json:"homepage,omitempty"`
}

func NormalizeDataSources(raw []DataSource) ([]DataSource, []string) {
	var out []DataSource
	var warnings []string
	seen := map[string]bool{}
	for _, src := range raw {
		name := strings.TrimSpace(src.Name)
		if name == "" {
			warnings = append(warnings, "dropping a data source with no name")
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			warnings = append(warnings, fmt.Sprintf("dropping duplicate data source %q", name))
			continue
		}
		if len(out) == maxDataSources {
			warnings = append(warnings, fmt.Sprintf("keeping the first %d data sources; dropping %q and the rest", maxDataSources, name))
			break
		}
		seen[key] = true
		homepage := NormalizeHomepage(src.Homepage)
		if homepage == "" && strings.TrimSpace(src.Homepage) != "" {
			warnings = append(warnings, fmt.Sprintf("data source %q homepage %q is not a public https URL; leaving it unset", name, src.Homepage))
		}
		out = append(out, DataSource{Name: name, Homepage: homepage})
	}
	return out, warnings
}

func decodeResearchDataSources(data json.RawMessage) ([]DataSource, []string) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, []string{"research data_sources is not a list; ignoring it"}
	}
	var out []DataSource
	var warnings []string
	for _, item := range items {
		item = bytes.TrimSpace(item)
		var name string
		if bytes.HasPrefix(item, []byte(`"`)) && json.Unmarshal(item, &name) == nil {
			out = append(out, DataSource{Name: name})
			continue
		}
		var src DataSource
		if bytes.HasPrefix(item, []byte("{")) && json.Unmarshal(item, &src) == nil {
			out = append(out, src)
			continue
		}
		warnings = append(warnings, fmt.Sprintf("ignoring research data source %s: expected a name or an object with name and homepage", item))
	}
	return out, warnings
}
