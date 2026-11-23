package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// MaxDataSources caps the list so catalog clients show the services a user
// would recognize, not every retailer a fan-out search touches.
const MaxDataSources = 12

// DataSource names one service a multi-service CLI reads data from. It is
// unrelated to the research `sources/` directory of downloaded reference repos.
type DataSource struct {
	Name     string `json:"name"`
	Homepage string `json:"homepage,omitempty"`
}

// NormalizeDataSources keeps the entries a catalog can trust, in order, and
// returns one warning per entry or field it dropped. It returns nil when
// nothing survives so the manifest omits the field.
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
		if len(out) == MaxDataSources {
			warnings = append(warnings, fmt.Sprintf("keeping the first %d data sources; dropping %q and the rest", MaxDataSources, name))
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

// decodeResearchDataSources reads research.json's agent-written list without
// ever failing: a bad value must not discard the rest of research. A string
// item is a name; an object item supplies name and homepage.
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
