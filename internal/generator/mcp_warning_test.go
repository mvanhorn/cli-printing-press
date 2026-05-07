package generator

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func TestWarnUnenrichedLargeMCPSurface(t *testing.T) {
	tests := []struct {
		name           string
		spec           *spec.APISpec
		wantWarning    bool
		wantContaining string
	}{
		{
			name:        "nil spec emits no warning",
			spec:        nil,
			wantWarning: false,
		},
		{
			name:        "small surface emits no warning",
			spec:        buildSpecWithEndpoints(10, spec.MCPConfig{}),
			wantWarning: false,
		},
		{
			name:        "exactly-at-threshold emits no warning",
			spec:        buildSpecWithEndpoints(50, spec.MCPConfig{}),
			wantWarning: false,
		},
		{
			name:           "above-threshold endpoint-mirror surface warns",
			spec:           buildSpecWithEndpoints(60, spec.MCPConfig{}),
			wantWarning:    true,
			wantContaining: "warning: spec exposes 60 MCP endpoint tools (>50",
		},
		{
			name: "above-threshold but already opted into code orchestration is silent",
			spec: buildSpecWithEndpoints(60, spec.MCPConfig{
				Orchestration: "code",
			}),
			wantWarning: false,
		},
		{
			name: "above-threshold with intent-only enrichment still warns (intents do not solve scale)",
			spec: buildSpecWithEndpoints(60, spec.MCPConfig{
				Intents: []spec.Intent{{Name: "lookup", Description: "x"}},
			}),
			wantWarning:    true,
			wantContaining: "endpoint-mirror surface burns agent context",
		},
		{
			name: "custom orchestration_threshold respected (raised above surface = silent)",
			spec: buildSpecWithEndpoints(60, spec.MCPConfig{
				OrchestrationThreshold: 100,
			}),
			wantWarning: false,
		},
		{
			name: "custom orchestration_threshold respected (lowered below surface = warns)",
			spec: buildSpecWithEndpoints(20, spec.MCPConfig{
				OrchestrationThreshold: 10,
			}),
			wantWarning:    true,
			wantContaining: "warning: spec exposes 20 MCP endpoint tools (>10",
		},
		{
			name:           "sub-resource endpoints counted toward the total",
			spec:           buildSpecWithSubResourceEndpoints(30, 30),
			wantWarning:    true,
			wantContaining: "warning: spec exposes 60 MCP endpoint tools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			warnUnenrichedLargeMCPSurface(tt.spec, &buf)
			got := buf.String()
			if tt.wantWarning {
				if got == "" {
					t.Fatalf("expected a warning, got none")
				}
				if !strings.Contains(got, tt.wantContaining) {
					t.Errorf("warning missing expected substring %q\nfull output:\n%s", tt.wantContaining, got)
				}
			} else {
				if got != "" {
					t.Errorf("expected no warning, got:\n%s", got)
				}
			}
		})
	}
}

func buildSpecWithEndpoints(n int, mcp spec.MCPConfig) *spec.APISpec {
	endpoints := make(map[string]spec.Endpoint, n)
	for i := range n {
		endpoints["ep_"+strconv.Itoa(i)] = spec.Endpoint{Method: "GET", Path: "/x"}
	}
	return &spec.APISpec{
		Resources: map[string]spec.Resource{
			"items": {Endpoints: endpoints},
		},
		MCP: mcp,
	}
}

func buildSpecWithSubResourceEndpoints(top, sub int) *spec.APISpec {
	topEndpoints := make(map[string]spec.Endpoint, top)
	for i := range top {
		topEndpoints["ep_"+strconv.Itoa(i)] = spec.Endpoint{Method: "GET", Path: "/x"}
	}
	subEndpoints := make(map[string]spec.Endpoint, sub)
	for i := range sub {
		subEndpoints["ep_"+strconv.Itoa(i)] = spec.Endpoint{Method: "GET", Path: "/y"}
	}
	return &spec.APISpec{
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: topEndpoints,
				SubResources: map[string]spec.Resource{
					"children": {Endpoints: subEndpoints},
				},
			},
		},
	}
}
