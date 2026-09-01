package generator

// librarySafeGoDirective is the go line emitted into every printed CLI.
// It is the public-library CI / stdlib CVE floor (GO-2026-6090, GO-2026-6218,
// GO-2026-6089, GO-2026-5972), not the print-host toolchain.
const librarySafeGoDirective = "1.26.6"

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

// selectEmittedGoDirective is the printed go.mod selection policy.
// The host or binary runtime version is accepted so tests can prove it is
// never copied in either direction: an older host must not freeze stdlib
// CVEs, and a newer host must not exceed library CI GOTOOLCHAIN=local.
func selectEmittedGoDirective(_ string) string {
	return librarySafeGoDirective
}
