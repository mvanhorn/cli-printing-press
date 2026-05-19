package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateEmitsRateLimitAuditFragment(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("rate-limit-audit")
	outputDir := filepath.Join(t.TempDir(), "rate-limit-audit-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientGo := string(clientSrc)

	assert.Contains(t, clientGo, "func emitRateLimitAudit(resp *http.Response)",
		"generated clients must expose response header rate-limit evidence for ConnectorRunner audit")
	assert.Contains(t, clientGo, `"rate_limit_remaining_after": remaining`,
		"rate-limit audit must use ConnectorRunner's expected audit field name")
	assert.Contains(t, clientGo, `fmt.Fprintf(os.Stderr, "[PP-AUDIT] %s\n", string(b))`,
		"rate-limit audit must be emitted as a machine-readable PP-AUDIT stderr fragment")

	doStart := strings.Index(clientGo, "func (c *Client) do(")
	require.NotEqual(t, -1, doStart, "client.go must contain Client.do")
	doBody := clientGo[doStart:]
	assert.Contains(t, doBody, "c.limiter.OnResponse(resp)\n\t\temitRateLimitAudit(resp)",
		"rate-limit audit must be emitted after every live provider response is parsed")
}
