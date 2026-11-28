package cartridge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	SchemaVersionV1 = "pp-cartridge/v1"

	DriverHTTP         = "http"
	DriverGraphQL      = "graphql"
	DriverLocalSQLite  = "local-sqlite"
	DriverLocalFiles   = "local-files"
	DriverOSAutomation = "os-automation"

	AuthStrategyNone          = "none"
	AuthStrategyEnvBearer     = "env_bearer"
	AuthStrategyEnvAPIKey     = "env_api_key"
	AuthStrategyOnePassword   = "op_secret"
	AuthStrategyHeader        = "header"
	AuthStrategyBrowserCookie = "browser_cookie"
	AuthStrategyAgentCookie   = "agentcookie"
)

// Cartridge is a declarative, inspectable, SHA-256-pinnable tool definition
// executed directly by the single `pp` runtime without requiring Go compilation.
type Cartridge struct {
	Schema      string                 `yaml:"schema" json:"schema"`
	Name        string                 `yaml:"name" json:"name"`
	Version     string                 `yaml:"version" json:"version"`
	Description string                 `yaml:"description" json:"description"`
	Category    string                 `yaml:"category,omitempty" json:"category,omitempty"`
	Homepage    string                 `yaml:"homepage,omitempty" json:"homepage,omitempty"`
	Domains     []string               `yaml:"domains,omitempty" json:"domains,omitempty"`
	Trust       TrustPolicy            `yaml:"trust" json:"trust"`
	Auth        AuthConfig             `yaml:"auth,omitempty" json:"auth,omitzero"`
	Commands    map[string]CommandSpec `yaml:"commands" json:"commands"`
	Source      string                 `yaml:"-" json:"source,omitempty"`
	SHA256      string                 `yaml:"-" json:"sha256,omitempty"`
}

// TrustPolicy constrains outbound network hosts and declares mutating commands
// so the runtime fails closed on unexpected destinations or side effects.
type TrustPolicy struct {
	AllowedHosts []string `yaml:"allowed_hosts,omitempty" json:"allowed_hosts,omitempty"`
	Mutates      []string `yaml:"mutates,omitempty" json:"mutates,omitempty"`
	ReadOnly     bool     `yaml:"read_only,omitempty" json:"read_only,omitempty"`
}

// AuthConfig describes how the cartridge obtains credentials or browser sessions.
type AuthConfig struct {
	Strategy           string   `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	EnvVar             string   `yaml:"env_var,omitempty" json:"env_var,omitempty"`
	OnePasswordRef     string   `yaml:"op_ref,omitempty" json:"op_ref,omitempty"`
	Header             string   `yaml:"header,omitempty" json:"header,omitempty"`
	Prefix             string   `yaml:"prefix,omitempty" json:"prefix,omitempty"`
	QueryParam         string   `yaml:"query_param,omitempty" json:"query_param,omitempty"`
	Domains            []string `yaml:"domains,omitempty" json:"domains,omitempty"`
	AgentCookieAdapter string   `yaml:"agentcookie_adapter,omitempty" json:"agentcookie_adapter,omitempty"`
}

// CommandSpec defines one invokable subcommand inside a Cartridge.
type CommandSpec struct {
	Description    string               `yaml:"description" json:"description"`
	Driver         string               `yaml:"driver,omitempty" json:"driver,omitempty"`
	Method         string               `yaml:"method,omitempty" json:"method,omitempty"`
	URL            string               `yaml:"url,omitempty" json:"url,omitempty"`
	Headers        map[string]string    `yaml:"headers,omitempty" json:"headers,omitempty"`
	Query          map[string]string    `yaml:"query,omitempty" json:"query,omitempty"`
	Body           string               `yaml:"body,omitempty" json:"body,omitempty"`
	GraphQLQuery   string               `yaml:"graphql_query,omitempty" json:"graphql_query,omitempty"`
	PersistedQuery string               `yaml:"persisted_query,omitempty" json:"persisted_query,omitempty"`
	Extract        string               `yaml:"extract,omitempty" json:"extract,omitempty"`
	CompactFields  []string             `yaml:"compact_fields,omitempty" json:"compact_fields,omitempty"`
	Params         map[string]ParamSpec `yaml:"params,omitempty" json:"params,omitempty"`
	Mutates        bool                 `yaml:"mutates,omitempty" json:"mutates,omitempty"`
	SyncDefault    bool                 `yaml:"sync_default,omitempty" json:"sync_default,omitempty"`

	// Local driver fields (zero binary decompilation; reads user-owned local data).
	DBPath   string `yaml:"db_path,omitempty" json:"db_path,omitempty"`
	SQL      string `yaml:"sql,omitempty" json:"sql,omitempty"`
	Glob     string `yaml:"glob,omitempty" json:"glob,omitempty"`
	Format   string `yaml:"format,omitempty" json:"format,omitempty"`
	OSEngine string `yaml:"os_engine,omitempty" json:"os_engine,omitempty"`
	OSScript string `yaml:"os_script,omitempty" json:"os_script,omitempty"`

	// LakeProjection maps extracted rows into the unified ~/.pp/lake.db FTS5 store.
	LakeProjection *LakeProjection `yaml:"lake_projection,omitempty" json:"lake_projection,omitempty"`
}

// ParamSpec declares a typed CLI/MCP parameter for a cartridge command.
type ParamSpec struct {
	Type        string `yaml:"type,omitempty" json:"type,omitempty"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Default     string `yaml:"default,omitempty" json:"default,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// LakeProjection maps command output fields into normalized Life Lake columns.
type LakeProjection struct {
	Entity    string            `yaml:"entity" json:"entity"`
	Key       string            `yaml:"key" json:"key"`
	Title     string            `yaml:"title,omitempty" json:"title,omitempty"`
	Summary   string            `yaml:"summary,omitempty" json:"summary,omitempty"`
	URL       string            `yaml:"url,omitempty" json:"url,omitempty"`
	Timestamp string            `yaml:"timestamp,omitempty" json:"timestamp,omitempty"`
	Fields    map[string]string `yaml:"fields,omitempty" json:"fields,omitempty"`
}

// DigestBytes returns the hex-encoded SHA-256 digest of raw cartridge bytes.
func DigestBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Parse decodes and validates a Cartridge from YAML bytes.
func Parse(raw []byte, source string) (*Cartridge, error) {
	var c Cartridge
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing cartridge %s: %w", source, err)
	}
	c.Source = source
	c.SHA256 = DigestBytes(raw)
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("validating cartridge %s: %w", source, err)
	}
	return &c, nil
}

// LoadFile reads, parses, and validates a cartridge from disk.
func LoadFile(path string) (*Cartridge, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw, path)
}

// MarshalYAML serializes a cartridge to canonical YAML bytes.
func (c *Cartridge) MarshalYAML() ([]byte, error) {
	return yaml.Marshal(c)
}

// Validate enforces structural, driver, and host-allowlist invariants.
func (c *Cartridge) Validate() error {
	if c.Schema != SchemaVersionV1 {
		return fmt.Errorf("unsupported cartridge schema %q (expected %q)", c.Schema, SchemaVersionV1)
	}
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("cartridge name is required")
	}
	if strings.ContainsAny(c.Name, "/\\ \t\n") {
		return fmt.Errorf("invalid cartridge name %q", c.Name)
	}
	if len(c.Commands) == 0 {
		return errors.New("cartridge must define at least one command")
	}
	for _, host := range c.Trust.AllowedHosts {
		h := strings.TrimSpace(host)
		if h == "" || h == "*" || h == "*.*" {
			return fmt.Errorf("unsafe wildcard host %q in trust.allowed_hosts", host)
		}
	}
	for cmdName, cmd := range c.Commands {
		driver := EffectiveDriver(cmd)
		switch driver {
		case DriverHTTP, DriverGraphQL:
			if strings.TrimSpace(cmd.URL) == "" {
				return fmt.Errorf("command %q requires url", cmdName)
			}
			u, err := url.Parse(cmd.URL)
			if err != nil || u.Hostname() == "" {
				return fmt.Errorf("command %q has invalid url %q", cmdName, cmd.URL)
			}
			if !c.HostAllowed(u.Hostname()) {
				return fmt.Errorf("command %q target host %q is not in trust.allowed_hosts", cmdName, u.Hostname())
			}
		case DriverLocalSQLite:
			if strings.TrimSpace(cmd.DBPath) == "" || strings.TrimSpace(cmd.SQL) == "" {
				return fmt.Errorf("local-sqlite command %q requires db_path and sql", cmdName)
			}
		case DriverLocalFiles:
			if strings.TrimSpace(cmd.Glob) == "" {
				return fmt.Errorf("local-files command %q requires glob", cmdName)
			}
		case DriverOSAutomation:
			if strings.TrimSpace(cmd.OSScript) == "" {
				return fmt.Errorf("os-automation command %q requires os_script", cmdName)
			}
		default:
			return fmt.Errorf("command %q has unsupported driver %q", cmdName, driver)
		}
	}
	return nil
}

// EffectiveDriver returns the normalized driver name for a command.
func EffectiveDriver(cmd CommandSpec) string {
	if strings.TrimSpace(cmd.Driver) != "" {
		return strings.ToLower(strings.TrimSpace(cmd.Driver))
	}
	if cmd.GraphQLQuery != "" || cmd.PersistedQuery != "" {
		return DriverGraphQL
	}
	return DriverHTTP
}

// IsMutating reports whether cmdName is declared as a mutating command either
// on the command itself or in the cartridge-level trust.mutates list.
func (c *Cartridge) IsMutating(cmdName string) bool {
	if cmd, ok := c.Commands[cmdName]; ok && cmd.Mutates {
		return true
	}
	return slices.Contains(c.Trust.Mutates, cmdName)
}

// HostAllowed reports whether hostname matches an entry in trust.allowed_hosts.
// Leading dot patterns (".example.com") match subdomains and the apex domain.
func (c *Cartridge) HostAllowed(hostname string) bool {
	target := strings.ToLower(strings.TrimSpace(hostname))
	if target == "" {
		return false
	}
	for _, allowed := range c.Trust.AllowedHosts {
		a := strings.ToLower(strings.TrimSpace(allowed))
		if a == "" {
			continue
		}
		if apex, ok := strings.CutPrefix(a, "."); ok {
			if target == apex || strings.HasSuffix(target, a) {
				return true
			}
			continue
		}
		if apex, ok := strings.CutPrefix(a, "*."); ok {
			if target == apex || strings.HasSuffix(target, "."+apex) {
				return true
			}
			continue
		}
		if target == a {
			return true
		}
	}
	return false
}

// MatchDomain reports whether host matches any declared domain or homepage host.
func (c *Cartridge) MatchDomain(host string) bool {
	h := normalizeHost(host)
	if h == "" {
		return false
	}
	for _, d := range c.Domains {
		dn := normalizeHost(d)
		if dn != "" && (h == dn || strings.HasSuffix(h, "."+dn)) {
			return true
		}
	}
	if c.Homepage != "" {
		if u, err := url.Parse(c.Homepage); err == nil {
			hp := normalizeHost(u.Hostname())
			if hp != "" && (h == hp || strings.HasSuffix(h, "."+hp)) {
				return true
			}
		}
	}
	return false
}

func normalizeHost(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	if idx := strings.IndexByte(h, '/'); idx >= 0 {
		h = h[:idx]
	}
	if idx := strings.IndexByte(h, ':'); idx >= 0 {
		h = h[:idx]
	}
	h = strings.TrimPrefix(h, "www.")
	h = strings.TrimPrefix(h, ".")
	return h
}

// HomeDir resolves the Printing Press 2.0 state directory (~/.pp or PP_HOME).
func HomeDir() (string, error) {
	if env := strings.TrimSpace(os.Getenv("PP_HOME")); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home dir: %w", err)
	}
	return filepath.Join(home, ".pp"), nil
}

// CartridgesDir returns the directory holding installed user cartridges.
func CartridgesDir() (string, error) {
	base, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cartridges"), nil
}
