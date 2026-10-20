package spec

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOmittedConfigDefaultsToTOML(t *testing.T) {
	const head = `name: omittedcfg
description: omitted config
version: "0.1.0"
base_url: https://api.example.com
auth:
  type: none
`
	const tail = `resources:
  items:
    description: Items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`
	tomlPath := "~/.config/omittedcfg-pp-cli/config.toml"
	jsonPath := "~/.config/omittedcfg-pp-cli/config.json"
	yamlPath := "~/.config/omittedcfg-pp-cli/config.yaml"

	tests := []struct {
		name       string
		configYAML string
		wantFormat string
		wantPath   string
	}{
		{name: "omitted block", wantFormat: ConfigFormatTOML, wantPath: tomlPath},
		{name: "empty mapping", configYAML: "config: {}\n", wantFormat: ConfigFormatTOML, wantPath: tomlPath},
		{name: "blank format and path", configYAML: "config:\n  format: \"\"\n  path: \"\"\n", wantFormat: ConfigFormatTOML, wantPath: tomlPath},
		{name: "whitespace format", configYAML: "config:\n  format: \"   \"\n", wantFormat: ConfigFormatTOML, wantPath: tomlPath},
		{name: "whitespace path", configYAML: "config:\n  path: \"   \"\n", wantFormat: ConfigFormatTOML, wantPath: tomlPath},
		{name: "explicit json", configYAML: "config:\n  format: json\n  path: ~/.config/custom/config.json\n", wantFormat: "json", wantPath: "~/.config/custom/config.json"},
		{name: "explicit json path omitted", configYAML: "config:\n  format: json\n", wantFormat: "json", wantPath: jsonPath},
		{name: "explicit yaml", configYAML: "config:\n  format: yaml\n  path: ~/.config/custom/config.yaml\n", wantFormat: "yaml", wantPath: "~/.config/custom/config.yaml"},
		{name: "explicit yaml path omitted", configYAML: "config:\n  format: yaml\n", wantFormat: "yaml", wantPath: yamlPath},
		{name: "explicit toml", configYAML: "config:\n  format: toml\n  path: ~/.config/custom/config.toml\n", wantFormat: ConfigFormatTOML, wantPath: "~/.config/custom/config.toml"},
		{name: "padded explicit json", configYAML: "config:\n  format: \"  json  \"\n  path: \"  ~/.config/custom/config.json  \"\n", wantFormat: "json", wantPath: "~/.config/custom/config.json"},
		{name: "unsupported format kept", configYAML: "config:\n  format: xml\n  path: ~/.config/custom/config.xml\n", wantFormat: "xml", wantPath: "~/.config/custom/config.xml"},
		{name: "path only", configYAML: "config:\n  path: ~/.config/custom/settings.toml\n", wantFormat: ConfigFormatTOML, wantPath: "~/.config/custom/settings.toml"},
		{name: "uppercase toml kept", configYAML: "config:\n  format: TOML\n  path: ~/.config/custom/config.toml\n", wantFormat: "TOML", wantPath: "~/.config/custom/config.toml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseBytes([]byte(head + tt.configYAML + tail))
			require.NoError(t, err)
			assert.Equal(t, tt.wantFormat, parsed.Config.Format)
			assert.Equal(t, tt.wantPath, parsed.Config.Path)
		})
	}
}

func TestApplyOmittedConfigDefaultsNilAndEmptyName(t *testing.T) {
	var missing *APISpec
	missing.ApplyOmittedConfigDefaults()

	empty := &APISpec{}
	empty.ApplyOmittedConfigDefaults()
	assert.Equal(t, ConfigFormatTOML, empty.Config.Format)
	assert.Empty(t, empty.Config.Path)

	empty.ApplyOmittedConfigDefaults()
	assert.Equal(t, ConfigFormatTOML, empty.Config.Format)
	assert.Empty(t, empty.Config.Path)
}

func TestApplyOmittedConfigDefaultsIsIdempotent(t *testing.T) {
	spec := &APISpec{Name: "demo"}
	spec.ApplyOmittedConfigDefaults()
	first := spec.Config
	spec.ApplyOmittedConfigDefaults()
	assert.Equal(t, first, spec.Config)
	assert.Equal(t, "~/.config/demo-pp-cli/config.toml", spec.Config.Path)
	assert.False(t, strings.Contains(spec.Config.Path, "config.json"))
}
