package pipeline

import "testing"

// TestReservedInternalPackages_LearnNamespace pins the carve-out for the
// generator-owned learn package. Files under internal/learn/ are emitted
// unconditionally by the generator; checks that scan agent-authored Go
// must treat the namespace as reserved so they do not flag generator
// output (or sibling files that route through learn helpers) as
// hand-rolled API behavior.
func TestReservedInternalPackages_LearnNamespace(t *testing.T) {
	if !reservedInternalPackages["learn"] {
		t.Fatalf("reservedInternalPackages[\"learn\"]: want true, got false")
	}
}

// TestReservedInternalPackages_CoreCarveOuts is a regression pin against
// accidental deletion of any of the established reserved entries. If one
// of these slips out of the map, every downstream check that filters on
// reservedInternalPackages starts flagging legitimate generator output.
func TestReservedInternalPackages_CoreCarveOuts(t *testing.T) {
	required := []string{
		"client", "store", "cliutil", "cache", "config",
		"mcp", "types", "share", "deliver", "profile",
		"feedback", "graphql", "learn",
	}
	for _, name := range required {
		if !reservedInternalPackages[name] {
			t.Errorf("reservedInternalPackages[%q]: want true, got false", name)
		}
	}
}
