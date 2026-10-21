package generator

// Public-library CI / stdlib CVE floor (GO-2026-6809). Do not derive from the
// print-host toolchain.
const librarySafeGoDirective = "1.26.10"

func currentGoDirectiveVersion() string {
	return librarySafeGoDirective
}

func currentGoToolchainVersion() string {
	return "go" + librarySafeGoDirective
}

func resolveCurrentGoDirectiveVersion() (string, error) {
	return librarySafeGoDirective, nil
}

func resolveCurrentGoToolchainVersion() (string, error) {
	return currentGoToolchainVersion(), nil
}

// Host or binary runtime version is accepted so tests can prove it is never
// copied: an older host must not freeze stdlib CVEs, and a newer host must
// not exceed library CI GOTOOLCHAIN=local.
func selectEmittedGoDirective(_ string) string {
	return librarySafeGoDirective
}
