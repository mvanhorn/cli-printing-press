package cli

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrintingPressSkillQuickstartFirstStepIsVerifySafe(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)

	content := string(data)
	require.Contains(t, content, "Step 1 of `quickstart` MUST be verify-safe")
	require.Contains(t, content, "Use `<cli> doctor --dry-run` as step 1")
	require.Contains(t, content, "Do not use `<cli> auth set-token <token>` as step 1")
	require.Contains(t, content, "Auth setup instructions belong in `auth_narrative` prose only")
}
