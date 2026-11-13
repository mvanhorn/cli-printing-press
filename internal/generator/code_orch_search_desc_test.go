package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestCodeOrchestrationSearchDescriptionNamesResources proves the emitted
// <api>_search description ends with the API's resource families, built from
// the endpoint registry when the tools are registered. Hosts that pick tools
// by matching words (BM25 over name and description) rank other tools above a
// search tool whose description names no resource for "<api> <resource>".
func TestCodeOrchestrationSearchDescriptionNamesResources(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("code-orch-desc")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources = map[string]spec.Resource{
		"tickets": {
			Description: "Support tickets",
			Endpoints: map[string]spec.Endpoint{
				"list":   {Method: "GET", Path: "/tickets", Description: "List tickets"},
				"get":    {Method: "GET", Path: "/tickets/{id}", Description: "Get a ticket"},
				"update": {Method: "PUT", Path: "/tickets/{id}", Description: "Update a ticket"},
			},
		},
		"users": {
			Description: "Agents and customers",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/users", Description: "List users"},
				"get":  {Method: "GET", Path: "/users/{id}", Description: "Get a user"},
			},
		},
		"kb": {
			Description: "Knowledge base",
			Endpoints: map[string]spec.Endpoint{
				"answer-create": {Method: "POST", Path: "/kb/answers", Description: "Create an answer"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	runtimeTest := `package mcp

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func TestCodeOrchestrationSearchDescriptionNamesResources(t *testing.T) {
	s := server.NewMCPServer("code-orch-desc", "test")
	RegisterCodeOrchestrationTools(s)
	var desc string
	for name, st := range s.ListTools() {
		if strings.HasSuffix(name, "_search") {
			desc = st.Tool.Description
		}
	}
	if want := " Resources: tickets, users, kb."; !strings.HasSuffix(desc, want) {
		t.Fatalf("search description = %q, want it to end with %q", desc, want)
	}
}

func TestCodeOrchestrationSearchResourcesRules(t *testing.T) {
	eps := func(ids ...string) []codeOrchEndpoint {
		out := make([]codeOrchEndpoint, 0, len(ids))
		for _, id := range ids {
			out = append(out, codeOrchEndpoint{ID: id})
		}
		return out
	}
	cases := map[string]struct {
		api  string
		eps  []codeOrchEndpoint
		want string
	}{
		"none": {"acme", nil, ""},
		"by count then name": {"acme", eps("users.get", "tickets.list", "tickets.get", "kb.answer-create", "users.list", "tickets.update"),
			" Resources: tickets, users, kb."},
		"api name and a shared tag dropped": {"acme", eps("acme-owners-crm.get", "acme-deals-crm.list", "acme-deals-crm.get",
			"acme-line-items-crm.list", "crm.batch", "acme.version"),
			" Resources: deals, acme, crm, line items, owners."},
		"a shared first word dropped": {"acme", eps("v2-users.get", "v2-tickets.get", "v2-tickets.list"),
			" Resources: tickets, users."},
		"a minority word kept": {"acme", eps("ticket-notes.get", "company-notes.get", "tickets.get", "companies.get", "users.get"),
			" Resources: companies, company notes, ticket notes, tickets, users."},
		"snake and kebab api name": {"acme-psa", eps("acme_psa_contracts.get", "acme-psa-tickets.get"),
			" Resources: contracts, tickets."},
	}
	for name, c := range cases {
		if got := codeOrchSearchResources(c.api, c.eps); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}

	var many []codeOrchEndpoint
	for i := 0; i < 200; i++ {
		many = append(many, codeOrchEndpoint{ID: "family-" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + strings.Repeat("x", i%7) + ".get"})
	}
	got := codeOrchSearchResources("acme", many)
	list := strings.TrimSuffix(strings.TrimPrefix(got, " Resources: "), ".")
	if !strings.HasSuffix(got, ", and more.") || len(list) > codeOrchSearchResourceCap || len(list) < codeOrchSearchResourceCap-40 {
		t.Fatalf("a long list must stop just under %d characters and say so; got %d: %q", codeOrchSearchResourceCap, len(list), got)
	}
	edge := codeOrchSearchResources("acme", []codeOrchEndpoint{{ID: strings.Repeat("a", 149) + ".get"}, {ID: strings.Repeat("b", 149) + ".get"}, {ID: "z.get"}})
	if want := " Resources: " + strings.Repeat("a", 149) + ", and more."; edge != want {
		t.Fatalf("the closing \", and more\" must fit the cap too; got %d characters: %q", len(edge), edge)
	}
	if again := codeOrchSearchResources("acme", many); again != got {
		t.Fatalf("the description is not deterministic:\n%q\n%q", got, again)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_search_desc_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^TestCodeOrchestrationSearch(DescriptionNamesResources|ResourcesRules)$", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}
