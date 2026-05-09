package platform

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecutablePathForGOOS(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		goos string
		want string
	}{
		{
			name: "windows appends exe",
			path: filepath.Join("tmp", "sample-cli"),
			goos: "windows",
			want: filepath.Join("tmp", "sample-cli.exe"),
		},
		{
			name: "windows preserves exe",
			path: filepath.Join("tmp", "sample-cli.exe"),
			goos: "windows",
			want: filepath.Join("tmp", "sample-cli.exe"),
		},
		{
			name: "darwin unchanged",
			path: filepath.Join("tmp", "sample-cli"),
			goos: "darwin",
			want: filepath.Join("tmp", "sample-cli"),
		},
		{
			name: "linux unchanged",
			path: filepath.Join("tmp", "sample-cli"),
			goos: "linux",
			want: filepath.Join("tmp", "sample-cli"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ExecutablePathForGOOS(tc.path, tc.goos))
		})
	}
}
