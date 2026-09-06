package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVerifyMode(t *testing.T) {
	const name = "PRINTING_PRESS_TEST_LIVE_CREDENTIAL"
	for _, tc := range []struct {
		name, key, env, value, mode string
		warn                        bool
	}{
		{"default mock", "", "", "", "mock", false},
		{"explicit key", "explicit-fixture", "", "", "live", false},
		{"explicit environment", "", name, "env-fixture", "live", false},
		{"empty environment", "", name, "", "mock", true},
		{"key wins", "explicit-fixture", name, "", "live", false},
		{"ambient credential is not opt in", "", "", "env-fixture", "mock", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(name, tc.value)
			cfg := VerifyConfig{APIKey: tc.key, EnvVar: tc.env}
			mode, detail := verifyMode(cfg)
			assert.Equal(t, tc.mode, mode)
			assert.Equal(t, tc.warn, detail != "")
			assert.NotContains(t, detail, "env-fixture")
			assert.NotContains(t, detail, "explicit-fixture")
			assert.Equal(t, tc.key, cfg.APIKey)
			if tc.warn {
				assert.Contains(t, detail, name)
			}
		})
	}
}
