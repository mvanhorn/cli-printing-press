package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedEndpointBudgetsFailClosedAndScopeVerifiedTenant(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("endpoint-budget-contract")
	apiSpec.MCP = spec.MCPConfig{Transport: []string{"stdio"}}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	clientSource := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSource, "func (c *Client) platformBudgetLocked(endpointClass string) (platform.EndpointBudget, error)")
	require.Contains(t, clientSource, `c.platformBudgets["*"]`)
	require.Contains(t, clientSource, "budget.Class = endpointClass")
	require.Contains(t, clientSource, "&platform.MissingEndpointBudgetError{EndpointClass: endpointClass}")
	require.NotContains(t, clientSource, "Steady: 2, Interval: time.Second, Burst: 2")

	rateLimitSource := readGeneratedFile(t, outputDir, "internal", "platform", "ratelimit.go")
	require.Contains(t, rateLimitSource, "type MissingEndpointBudgetError struct")
	require.Contains(t, rateLimitSource, "len(session.ObservedIdentity) == 0")
	require.Contains(t, rateLimitSource, "json.Marshal(session.ObservedIdentity)")
	require.Contains(t, rateLimitSource, "string(observedIdentity)")
	require.Equal(t, 1, strings.Count(rateLimitSource, "else if ledger.UpdatedAt.Before(now)"))
	require.Equal(t, 1, strings.Count(rateLimitSource, "ledger.UpdatedAt = reservationStart.Add(refill)"))

	budgetTest := readGeneratedFile(t, outputDir, "internal", "client", "platform_budget_test.go")
	require.Contains(t, budgetTest, "TestPlatformBudgetLookupContract")

	rateLimitTest := readGeneratedFile(t, outputDir, "internal", "client", "platform_rate_limit_test.go")
	require.Contains(t, rateLimitTest, `SetPlatformEndpointBudget(platform.EndpointBudget{Class: "*"`)

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/client", "-run", "^TestPlatform(BudgetLookupContract|RateLimit)", "-count=1")
}
