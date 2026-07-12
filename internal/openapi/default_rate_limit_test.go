package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDefaultRateLimitExtension(t *testing.T) {
	t.Parallel()

	base := func(ext string) []byte {
		return []byte(`
openapi: 3.0.3
` + ext + `
info:
  title: RL API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: ok
`)
	}

	t.Run("auto string", func(t *testing.T) {
		parsed, err := Parse(base(`x-pp-default-rate-limit: auto`))
		require.NoError(t, err)
		assert.Equal(t, "auto", parsed.DefaultRateLimit)
	})
	t.Run("numeric", func(t *testing.T) {
		parsed, err := Parse(base(`x-pp-default-rate-limit: 3`))
		require.NoError(t, err)
		assert.Equal(t, "3", parsed.DefaultRateLimit)
	})
	t.Run("numeric string", func(t *testing.T) {
		parsed, err := Parse(base(`x-pp-default-rate-limit: "2.5"`))
		require.NoError(t, err)
		assert.Equal(t, "2.5", parsed.DefaultRateLimit)
	})
	t.Run("absent", func(t *testing.T) {
		parsed, err := Parse(base(``))
		require.NoError(t, err)
		assert.Equal(t, "", parsed.DefaultRateLimit)
	})
	t.Run("invalid word", func(t *testing.T) {
		_, err := Parse(base(`x-pp-default-rate-limit: sometimes`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `x-pp-default-rate-limit must be "auto" or a non-negative number`)
	})
	t.Run("negative", func(t *testing.T) {
		_, err := Parse(base(`x-pp-default-rate-limit: -1`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-negative")
	})
}
