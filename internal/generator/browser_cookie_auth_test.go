package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func TestIsBrowserCookieAuthRequiresCookiePlacement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth spec.AuthConfig
		want bool
	}{
		{
			name: "browser cookie auth",
			auth: spec.AuthConfig{Type: "cookie", In: "cookie", Header: "Cookie"},
			want: true,
		},
		{
			name: "empty header browser cookie auth",
			auth: spec.AuthConfig{Type: "cookie", In: "cookie"},
			want: true,
		},
		{
			name: "cookie typed header credential",
			auth: spec.AuthConfig{Type: "cookie", In: "header", Header: "Cookie"},
			want: false,
		},
		{
			name: "named cookie auth",
			auth: spec.AuthConfig{Type: "cookie", In: "cookie", Header: "session_id"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isBrowserCookieAuth(tt.auth); got != tt.want {
				t.Fatalf("isBrowserCookieAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}
