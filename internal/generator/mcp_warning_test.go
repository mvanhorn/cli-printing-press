package generator

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func TestApplyLargeMCPSurfaceDefault(t *testing.T) {
	tests := []struct {
		name            string
		spec            *spec.APISpec
		wantInfo        bool
		wantContaining  string
		wantNotContain  string
		wantOrch        string
		wantEndpoint    string
		wantTransport   []string
		wantNoMCPChange bool
	}{
		{
			name:            "nil spec emits no info",
			spec:            nil,
			wantNoMCPChange: true,
		},
		{
			name:            "small surface emits no info",
			spec:            buildSpecWithEndpoints(10, spec.MCPConfig{}),
			wantNoMCPChange: true,
		},
		{
			name:            "exactly-at-threshold emits no info",
			spec:            buildSpecWithEndpoints(50, spec.MCPConfig{}),
			wantNoMCPChange: true,
		},
		{
			name:           "above-threshold default surface applies code orchestration",
			spec:           buildSpecWithEndpoints(60, spec.MCPConfig{}),
			wantInfo:       true,
			wantContaining: "info: applied Cloudflare MCP pattern (60 endpoints > 50 threshold)",
			wantOrch:       "code",
			wantEndpoint:   "hidden",
			wantTransport:  []string{"stdio", "http"},
		},
		{
			name: "above-threshold but already opted into code orchestration is unchanged",
			spec: buildSpecWithEndpoints(60, spec.MCPConfig{
				Orchestration: "code",
			}),
			wantOrch: "code",
		},
		{
			name: "above-threshold explicit endpoint mirror opt-out is unchanged",
			spec: buildSpecWithEndpoints(60, spec.MCPConfig{
				Orchestration: "endpoint-mirror",
			}),
			wantOrch: "endpoint-mirror",
		},
		{
			name: "above-threshold explicit intent orchestration is unchanged",
			spec: buildSpecWithEndpoints(60, spec.MCPConfig{
				Orchestration: "intent",
			}),
			wantOrch: "intent",
		},
		{
			name: "above-threshold with intent-only enrichment still applies code orchestration",
			spec: buildSpecWithEndpoints(60, spec.MCPConfig{
				Intents: []spec.Intent{{Name: "lookup", Description: "x"}},
			}),
			wantInfo:       true,
			wantContaining: "applied Cloudflare MCP pattern",
			wantOrch:       "code",
			wantEndpoint:   "hidden",
			wantTransport:  []string{"stdio", "http"},
		},
		{
			name: "custom orchestration_threshold respected (raised above surface = unchanged)",
			spec: buildSpecWithEndpoints(60, spec.MCPConfig{
				OrchestrationThreshold: 100,
			}),
			wantNoMCPChange: true,
		},
		{
			name: "custom orchestration_threshold respected (lowered below surface = applies)",
			spec: buildSpecWithEndpoints(20, spec.MCPConfig{
				OrchestrationThreshold: 10,
			}),
			wantInfo:       true,
			wantContaining: "20 endpoints > 10 threshold",
			wantOrch:       "code",
			wantEndpoint:   "hidden",
			wantTransport:  []string{"stdio", "http"},
		},
		{
			name:           "sub-resource endpoints counted toward the total",
			spec:           buildSpecWithSubResourceEndpoints(30, 30),
			wantInfo:       true,
			wantContaining: "60 endpoints > 50 threshold",
			wantOrch:       "code",
			wantEndpoint:   "hidden",
			wantTransport:  []string{"stdio", "http"},
		},
		{
			name: "explicit transport is preserved when applying the orchestration default",
			spec: buildSpecWithEndpoints(60, spec.MCPConfig{
				Transport: []string{"stdio"},
			}),
			wantInfo:       true,
			wantContaining: "preserved explicit transport",
			wantNotContain: "transport [stdio,http]",
			wantOrch:       "code",
			wantEndpoint:   "hidden",
			wantTransport:  []string{"stdio"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			applyLargeMCPSurfaceDefault(tt.spec, &buf)
			got := buf.String()
			if tt.wantInfo {
				if got == "" {
					t.Fatalf("expected an info line, got none")
				}
				if !strings.Contains(got, tt.wantContaining) {
					t.Errorf("info line missing expected substring %q\nfull output:\n%s", tt.wantContaining, got)
				}
				if tt.wantNotContain != "" && strings.Contains(got, tt.wantNotContain) {
					t.Errorf("info line contained unexpected substring %q\nfull output:\n%s", tt.wantNotContain, got)
				}
			} else {
				if got != "" {
					t.Errorf("expected no info line, got:\n%s", got)
				}
			}
			if tt.spec == nil {
				return
			}
			if tt.wantNoMCPChange {
				if tt.spec.MCP.Orchestration != "" || tt.spec.MCP.EndpointTools != "" || len(tt.spec.MCP.Transport) != 0 {
					t.Fatalf("expected MCP config to remain empty, got %+v", tt.spec.MCP)
				}
				return
			}
			if tt.wantOrch != "" && tt.spec.MCP.Orchestration != tt.wantOrch {
				t.Fatalf("orchestration = %q, want %q", tt.spec.MCP.Orchestration, tt.wantOrch)
			}
			if tt.wantEndpoint != "" && tt.spec.MCP.EndpointTools != tt.wantEndpoint {
				t.Fatalf("endpoint_tools = %q, want %q", tt.spec.MCP.EndpointTools, tt.wantEndpoint)
			}
			if tt.wantTransport != nil && !slices.Equal(tt.spec.MCP.Transport, tt.wantTransport) {
				t.Fatalf("transport = %#v, want %#v", tt.spec.MCP.Transport, tt.wantTransport)
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
