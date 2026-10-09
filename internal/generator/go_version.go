package generator

// Public-library CI / stdlib CVE floor (GO-2026-6599, GO-2026-6600,
// GO-2026-6601, GO-2026-6602, GO-2026-6603, GO-2026-6604, GO-2026-6605,
// GO-2026-6607, GO-2026-6608, GO-2026-6609, GO-2026-6610, GO-2026-6611,
// GO-2026-6612, GO-2026-6613, GO-2026-6617). Do not derive from the
// print-host toolchain.
const librarySafeGoDirective = "1.26.9"

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
