package pipeline

import (
	"strings"
)

// filterVerifyEnv returns a copy of env with every entry whose key matches a
// verify-mode env var dropped. Live verification runners (live_dogfood,
// workflow_verify) use this to construct subprocess envs that cannot be
// short-circuited by PRINTING_PRESS_VERIFY=1 inherited from the operator's
// shell.
//
// The transport-layer short-circuit added in `client.go.tmpl` short-circuits
// mutating HTTP verbs when PRINTING_PRESS_VERIFY=1 is set. That is the
// correct behavior for verify-mode mock-mode runs. But the live verifiers
// exercise real (or near-real) destructive behavior and must not silently
// noop when the operator happens to have PRINTING_PRESS_VERIFY=1 in their
// shell (parent process, CI runner, container image). Stripping both vars
// at the exec boundary is defense in depth.
//
// Filter is exact-key match on the bytes before the first '=' to avoid
// accidentally dropping unrelated env vars whose values contain the literal
// string.
func filterVerifyEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			out = append(out, entry)
			continue
		}
		if key == "PRINTING_PRESS_VERIFY" || key == "PRINTING_PRESS_VERIFY_LIVE_HTTP" {
			continue
		}
		out = append(out, entry)
	}
	return out
}
