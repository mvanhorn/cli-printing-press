package cli

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseValidationMode(t *testing.T) {
	valid := map[string]generator.ValidationMode{
		"":          generator.ModeBinary,
		"binary":    generator.ModeBinary,
		"go-run":    generator.ModeGoRun,
		"docker":    generator.ModeDocker,
		"skip-exec": generator.ModeSkipExec,
	}
	for in, want := range valid {
		got, err := parseValidationMode(in)
		require.NoErrorf(t, err, "parseValidationMode(%q)", in)
		assert.Equalf(t, want, got, "parseValidationMode(%q)", in)
	}

	_, err := parseValidationMode("bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --validation-mode")
}
