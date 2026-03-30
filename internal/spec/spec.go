package spec

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type APISpec struct {
	Name          string              `yaml:"name"`
	Description   string              `yaml:"description"`
	Version       string              `yaml:"version"`
	BaseURL       string              `yaml:"base_url"`
	BasePath      string              `yaml:"base_path,omitempty"`
	Owner         string              `yaml:"owner,omitempty"`          // GitHub owner for import paths and Homebrew tap
	SpecSource    string              `yaml:"spec_source,omitempty"`    // official, community, sniffed, docs — affects generated client defaults
	ClientPattern string              `yaml:"client_pattern,omitempty"` // rest (default), proxy-envelope — affects generated HTTP client
	ProxyRoutes   map[string]string   `yaml:"proxy_routes,omitempty"`   // path prefix → service name for proxy-envelope routing
	Auth          AuthConfig          `yaml:"auth"`
	Config        ConfigSpec          `yaml:"config"`
	Resources     map[string]Resource `yaml:"resources"`
	Types         map[string]TypeDef  `yaml:"types"`
}

type AuthConfig struct {
	Type             string   `yaml:"type"` // api_key, oauth2, bearer_token, cookie, none
	Header           string   `yaml:"header"`
	Format           string   `yaml:"format"`
	EnvVars          []string `yaml:"env_vars"`
	Scheme           string   `yaml:"scheme,omitempty"` // OpenAPI security scheme name
	In               string   `yaml:"in,omitempty"`     // header, query, cookie
	AuthorizationURL string   `yaml:"authorization_url,omitempty"`
	TokenURL         string   `yaml:"token_url,omitempty"`
	Scopes           []string `yaml:"scopes,omitempty"`
}

type ConfigSpec struct {
	Format string `yaml:"format"` // toml, yaml
	Path   string `yaml:"path"`
}

type Resource struct {
	Description  string              `yaml:"description"`
	Endpoints    map[string]Endpoint `yaml:"endpoints"`
	SubResources map[string]Resource `yaml:"sub_resources,omitempty"`
}

type Endpoint struct {
	Method       string            `yaml:"method"`
	Path         string            `yaml:"path"`
	Description  string            `yaml:"description"`
	Params       []Param           `yaml:"params"`
	Body         []Param           `yaml:"body"`
	Response     ResponseDef       `yaml:"response"`
	Pagination   *Pagination       `yaml:"pagination"`
	ResponsePath string            `yaml:"response_path,omitempty"` // path to extract data array from response (e.g., "data", "results.items")
	Meta         map[string]string `yaml:"meta,omitempty"`          // per-endpoint metadata (e.g., source_tier, source_count from crowd-sniff)
	Alias        string            `yaml:"-"`                       // computed, not from YAML
}

type Param struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Required    bool     `yaml:"required"`
	Positional  bool     `yaml:"positional"`
	Default     any      `yaml:"default"`
	Description string   `yaml:"description"`
	Fields      []Param  `yaml:"fields"`           // for nested objects
	Enum        []string `yaml:"enum,omitempty"`   // enum constraints for the parameter
	Format      string   `yaml:"format,omitempty"` // OpenAPI format hints (date-time, email, uri, etc.)
}

type ResponseDef struct {
	Type string `yaml:"type"` // object, array
	Item string `yaml:"item"` // type name
}

type Pagination struct {
	Type           string `yaml:"type"`             // cursor, offset, page_token
	LimitParam     string `yaml:"limit_param"`      // query param name for page size (limit, maxResults, pageSize)
	CursorParam    string `yaml:"cursor_param"`     // query param name for cursor (after, pageToken, offset)
	NextCursorPath string `yaml:"next_cursor_path"` // response field with next cursor (nextPageToken, cursor)
	HasMoreField   string `yaml:"has_more_field"`   // response field indicating more pages (has_more)
}

type TypeDef struct {
	Fields []TypeField `yaml:"fields"`
}

type TypeField struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

func Parse(path string) (*APISpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return ParseBytes(data)
}

func ParseBytes(data []byte) (*APISpec, error) {
	var s APISpec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}
	return &s, nil
}

func (s *APISpec) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	// Parser fallback may supply a placeholder base_url when the source spec omits servers.
	if s.BaseURL == "" && s.BasePath == "" {
		return fmt.Errorf("base_url is required")
	}
	if len(s.Resources) == 0 {
		return fmt.Errorf("at least one resource is required")
	}
	for name, r := range s.Resources {
		if len(r.Endpoints) == 0 && len(r.SubResources) == 0 {
			return fmt.Errorf("resource %q has no endpoints", name)
		}
		for eName, e := range r.Endpoints {
			if e.Method == "" {
				return fmt.Errorf("resource %q endpoint %q: method is required", name, eName)
			}
			if e.Path == "" {
				return fmt.Errorf("resource %q endpoint %q: path is required", name, eName)
			}
		}
		for subName, sub := range r.SubResources {
			if len(sub.Endpoints) == 0 {
				return fmt.Errorf("resource %q sub-resource %q has no endpoints", name, subName)
			}
			for eName, e := range sub.Endpoints {
				if e.Method == "" {
					return fmt.Errorf("resource %q sub-resource %q endpoint %q: method is required", name, subName, eName)
				}
				if e.Path == "" {
					return fmt.Errorf("resource %q sub-resource %q endpoint %q: path is required", name, subName, eName)
				}
			}
		}
	}
	return nil
}
