package govulncheck

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolModuleIsPinned(t *testing.T) {
	assert.NotContains(t, ToolModule, "@latest")
	assert.True(t, strings.HasPrefix(ToolVersion, "v"))
	assert.Equal(t, "golang.org/x/vuln/cmd/govulncheck@"+ToolVersion, ToolModule)
}

func TestGoRunArgsUsesDefaultMode(t *testing.T) {
	args := GoRunArgs("./...")
	assert.Equal(t, []string{"run", ToolModule, "./..."}, args)
	assert.NotContains(t, args, "-show")
	assert.NotContains(t, args, "verbose")
}
