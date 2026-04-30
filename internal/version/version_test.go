package version

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionIsValidSemver(t *testing.T) {
	assert.NotEmpty(t, Version)
	assert.Regexp(t, regexp.MustCompile(`^\d+\.\d+\.\d+$`), Version)
}

func TestGetReturnsVersion(t *testing.T) {
	assert.Equal(t, Version, Get())
}

func TestVersionFromBuildInfo(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"devel", "(devel)", ""},
		{"tagged release", "v2.3.6", "2.3.6"},
		{"tagged release no v", "2.3.6", "2.3.6"},
		{"v3 tagged release", "v3.0.0", "3.0.0"},
		// Pseudo-versions in every form Go can synthesize. See
		// https://go.dev/ref/mod#pseudo-versions.
		{"pseudo no prior tag", "v0.0.0-20260328120000-abcdef123456", ""},
		{"pseudo after release", "v1.3.3-0.20260426011609-42b0f1f4a92a", ""},
		{"pseudo after prerelease", "v2.4.0-pre.0.20260426011609-42b0f1f4a92a", ""},
		{"pseudo without v prefix", "1.3.3-0.20260426011609-42b0f1f4a92a", ""},
		// Builds off a dirty working tree get a `+dirty` semver build-metadata
		// suffix. Any `+<meta>` suffix should be stripped before classification
		// so it does not defeat the pseudo-version match.
		{"pseudo dirty after release", "v2.4.1-0.20260430120000-abcdef123456+dirty", ""},
		{"pseudo dirty no prior tag", "v0.0.0-20260328120000-abcdef123456+dirty", ""},
		{"pseudo dirty without v prefix", "1.3.3-0.20260426011609-42b0f1f4a92a+dirty", ""},
		{"tagged release dirty", "v3.0.0+dirty", "3.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, versionFromBuildInfo(tt.in))
		})
	}
}
