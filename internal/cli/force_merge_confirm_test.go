package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfirmForceHandAuthoredDeletesRequiresYesWhenNonInteractive(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := confirmForceHandAuthoredDeletes(false, []string{"internal/handpkg/client.go"}, nil, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pass --yes to confirm")
	assert.Contains(t, out.String(), forceHandAuthoredDeleteWarning)
	assert.Contains(t, out.String(), "internal/handpkg/client.go")
}

func TestConfirmForceHandAuthoredDeletesYesSkipsPrompt(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	require.NoError(t, confirmForceHandAuthoredDeletes(true, []string{"internal/cli/handapp_auth.go"}, nil, &out))
	assert.Contains(t, out.String(), forceHandAuthoredDeleteWarning)
	assert.Contains(t, out.String(), "internal/cli/handapp_auth.go")
}

func TestConfirmForceHandAuthoredDeletesInteractiveYesAndNo(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	require.NoError(t, confirmForceHandAuthoredDeletes(false, []string{"internal/store/handapp_migrations.go"}, strings.NewReader("yes\n"), &out))

	out.Reset()
	err := confirmForceHandAuthoredDeletes(false, []string{"internal/store/handapp_migrations.go"}, strings.NewReader("n\n"), &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestConfirmForceHandAuthoredDeletesNoopWhenEmpty(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	require.NoError(t, confirmForceHandAuthoredDeletes(false, nil, nil, &out))
	assert.Empty(t, out.String())
}
