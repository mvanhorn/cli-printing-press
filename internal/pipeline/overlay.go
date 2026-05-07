package pipeline

// SpecOverlay represents enrichments to apply on top of an original spec.
// Non-nil fields override the original when merged.
type SpecOverlay struct {
	Resources map[string]ResourceOverlay `yaml:"resources,omitempty"`
}

// ResourceOverlay enriches a single resource.
type ResourceOverlay struct {
	Description *string                    `yaml:"description,omitempty"`
	Endpoints   map[string]EndpointOverlay `yaml:"endpoints,omitempty"`
}

// EndpointOverlay enriches a single endpoint.
type EndpointOverlay struct {
	Description *string      `yaml:"description,omitempty"`
	Params      []ParamPatch `yaml:"params,omitempty"`
	Body        []ParamPatch `yaml:"body,omitempty"`
}

// ParamPatch modifies a single parameter.
type ParamPatch struct {
	Name          string    `yaml:"name"`
	Default       *string   `yaml:"default,omitempty"`
	FlagName      *string   `yaml:"flag_name,omitempty"`
	ClearFlagName bool      `yaml:"clear_flag_name,omitempty"`
	Aliases       *[]string `yaml:"aliases,omitempty"`
}
