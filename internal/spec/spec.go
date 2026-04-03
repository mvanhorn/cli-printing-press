package spec

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type APISpec struct {
	Name          string              `yaml:"name" json:"name"`
	Description   string              `yaml:"description" json:"description"`
	Version       string              `yaml:"version" json:"version"`
	BaseURL       string              `yaml:"base_url" json:"base_url"`
	BasePath      string              `yaml:"base_path,omitempty" json:"base_path,omitempty"`
	Owner         string              `yaml:"owner,omitempty" json:"owner,omitempty"`                   // GitHub owner for import paths and Homebrew tap
	SpecSource    string              `yaml:"spec_source,omitempty" json:"spec_source,omitempty"`       // official, community, sniffed, docs — affects generated client defaults
	ClientPattern string              `yaml:"client_pattern,omitempty" json:"client_pattern,omitempty"` // rest (default), proxy-envelope — affects generated HTTP client
	ProxyRoutes   map[string]string   `yaml:"proxy_routes,omitempty" json:"proxy_routes,omitempty"`     // path prefix → service name for proxy-envelope routing
	Auth          AuthConfig          `yaml:"auth" json:"auth"`
	Config        ConfigSpec          `yaml:"config" json:"config"`
	Resources     map[string]Resource `yaml:"resources" json:"resources"`
	Types         map[string]TypeDef  `yaml:"types" json:"types"`
}

type AuthConfig struct {
	Type             string   `yaml:"type" json:"type"` // api_key, oauth2, bearer_token, cookie, composed, none
	Header           string   `yaml:"header" json:"header"`
	Format           string   `yaml:"format" json:"format"`
	EnvVars          []string `yaml:"env_vars" json:"env_vars"`
	Scheme           string   `yaml:"scheme,omitempty" json:"scheme,omitempty"`   // OpenAPI security scheme name
	In               string   `yaml:"in,omitempty" json:"in,omitempty"`           // header, query, cookie
	KeyURL           string   `yaml:"key_url,omitempty" json:"key_url,omitempty"` // URL where users can register for an API key
	AuthorizationURL string   `yaml:"authorization_url,omitempty" json:"authorization_url,omitempty"`
	TokenURL         string   `yaml:"token_url,omitempty" json:"token_url,omitempty"`
	Scopes           []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	CookieDomain     string   `yaml:"cookie_domain,omitempty" json:"cookie_domain,omitempty"` // domain to read browser cookies from (e.g. ".notion.so")
	Cookies          []string `yaml:"cookies,omitempty" json:"cookies,omitempty"`             // named cookies to extract for composed auth (e.g. ["customerId", "authToken"])
}

type ConfigSpec struct {
	Format string `yaml:"format" json:"format"` // toml, yaml
	Path   string `yaml:"path" json:"path"`
}

type Resource struct {
	Description  string              `yaml:"description" json:"description"`
	Endpoints    map[string]Endpoint `yaml:"endpoints" json:"endpoints"`
	SubResources map[string]Resource `yaml:"sub_resources,omitempty" json:"sub_resources,omitempty"`
}

type Endpoint struct {
	Method       string            `yaml:"method" json:"method"`
	Path         string            `yaml:"path" json:"path"`
	Description  string            `yaml:"description" json:"description"`
	Params       []Param           `yaml:"params" json:"params"`
	Body         []Param           `yaml:"body" json:"body"`
	Response     ResponseDef       `yaml:"response" json:"response"`
	Pagination   *Pagination       `yaml:"pagination" json:"pagination"`
	ResponsePath string            `yaml:"response_path,omitempty" json:"response_path,omitempty"` // path to extract data array from response (e.g., "data", "results.items")
	Meta         map[string]string `yaml:"meta,omitempty" json:"meta,omitempty"`                   // per-endpoint metadata (e.g., source_tier, source_count from crowd-sniff)
	Alias        string            `yaml:"-" json:"-"`                                             // computed, not from YAML
}

type Param struct {
	Name        string   `yaml:"name" json:"name"`
	Type        string   `yaml:"type" json:"type"`
	Required    bool     `yaml:"required" json:"required"`
	Positional  bool     `yaml:"positional" json:"positional"`
	PathParam   bool     `yaml:"path_param,omitempty" json:"path_param,omitempty"` // true for path params rendered as flags (e.g., pagination)
	Default     any      `yaml:"default" json:"default"`
	Description string   `yaml:"description" json:"description"`
	Fields      []Param  `yaml:"fields" json:"fields"`                     // for nested objects
	Enum        []string `yaml:"enum,omitempty" json:"enum,omitempty"`     // enum constraints for the parameter
	Format      string   `yaml:"format,omitempty" json:"format,omitempty"` // OpenAPI format hints (date-time, email, uri, etc.)
}

type ResponseDef struct {
	Type string `yaml:"type" json:"type"` // object, array
	Item string `yaml:"item" json:"item"` // type name
}

type Pagination struct {
	Type           string `yaml:"type" json:"type"`                         // cursor, offset, page_token
	LimitParam     string `yaml:"limit_param" json:"limit_param"`           // query param name for page size (limit, maxResults, pageSize)
	CursorParam    string `yaml:"cursor_param" json:"cursor_param"`         // query param name for cursor (after, pageToken, offset)
	NextCursorPath string `yaml:"next_cursor_path" json:"next_cursor_path"` // response field with next cursor (nextPageToken, cursor)
	HasMoreField   string `yaml:"has_more_field" json:"has_more_field"`     // response field indicating more pages (has_more)
}

type TypeDef struct {
	Fields []TypeField `yaml:"fields" json:"fields"`
}

type TypeField struct {
	Name string `yaml:"name" json:"name"`
	Type string `yaml:"type" json:"type"`
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
	yamlErr := yaml.Unmarshal(data, &s)
	if yamlErr != nil || len(s.Resources) == 0 {
		// Try JSON round-trip: yaml → map → json → struct.
		// This handles YAML style variations (flow arrays, Python-style
		// quoting, non-standard indentation) that can cause struct mapping
		// to silently produce empty fields even though the YAML is valid.
		var raw map[string]any
		if yaml.Unmarshal(data, &raw) == nil && len(raw) > 0 {
			if jsonBytes, err := json.Marshal(raw); err == nil {
				var fallback APISpec
				if json.Unmarshal(jsonBytes, &fallback) == nil && len(fallback.Resources) > 0 {
					s = fallback
					yamlErr = nil
				}
			}
		}
	}
	if yamlErr != nil {
		return nil, fmt.Errorf("parsing yaml: %w", yamlErr)
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
