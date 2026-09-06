package pipeline

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyMode(t *testing.T) {
	const name = "PRINTING_PRESS_TEST_LIVE_CREDENTIAL"
	const secret = "env-fixture"
	for _, tc := range []struct {
		name   string
		key    string
		env    string
		setEnv bool
		value  string
		mode   string
		warn   bool
	}{
		{name: "default mock", mode: "mock"},
		{name: "explicit key", key: "explicit-fixture", mode: "live"},
		{name: "explicit environment", env: name, setEnv: true, value: secret, mode: "live"},
		{name: "empty environment", env: name, setEnv: true, value: "", mode: "mock", warn: true},
		{name: "unset environment", env: name, mode: "mock", warn: true},
		{name: "key wins", key: "explicit-fixture", env: name, setEnv: true, value: "", mode: "live"},
		{name: "ambient credential is not opt in", setEnv: true, value: secret, mode: "mock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv(name, tc.value)
			} else {
				t.Setenv(name, "sentinel")
				require.NoError(t, os.Unsetenv(name))
			}
			cfg := VerifyConfig{APIKey: tc.key, EnvVar: tc.env}
			mode, detail := verifyMode(cfg)
			assert.Equal(t, tc.mode, mode)
			assert.Equal(t, tc.warn, detail != "")
			assert.NotContains(t, detail, secret)
			assert.NotContains(t, detail, "explicit-fixture")
			assert.Equal(t, tc.key, cfg.APIKey)
			assert.NotEqual(t, secret, cfg.APIKey)
			if tc.warn {
				assert.Contains(t, detail, name)
			}
		})
	}
}
