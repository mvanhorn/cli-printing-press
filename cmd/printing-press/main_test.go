package main

import "testing"

func TestIsCatalogInstallerCommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "list", args: []string{"list"}, want: true},
		{name: "search", args: []string{"search", "travel"}, want: true},
		{name: "install", args: []string{"install", "airbnb"}, want: true},
		{name: "root flag before catalog command", args: []string{"--version", "list"}, want: true},
		{name: "future catalog command", args: []string{"update", "airbnb"}, want: true},
		{name: "generator command", args: []string{"generate"}, want: false},
		{name: "flag value before generator command", args: []string{"--", "list"}, want: false},
		{name: "empty args", args: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCatalogInstallerCommand(tt.args); got != tt.want {
				t.Fatalf("isCatalogInstallerCommand(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
