package version

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionIsValidSemver(t *testing.T) {
	assert.NotEmpty(t, Version)
	assert.Regexp(t, regexp.MustCompile(`^\d+\.\d+\.\d+`), Version)
}

func TestGetReturnsVersion(t *testing.T) {
	assert.Equal(t, Version, Get())
}
