package authdoctor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// minLengthByType gives the minimum expected length of a well-formed
// credential for common auth types. Values shorter than the threshold
// produce StatusSuspicious with a "too short" reason. These are
// heuristic nudges, not validation; false positives are acceptable.
var minLengthByType = map[string]int{
	"api_key":      8,
	"bearer_token": 20,
}

// getEnv looks up an env var. It is parameterised on the classifier so
// tests can inject a synthetic environment without touching os.Setenv.
type getEnv func(string) string

// Classify inspects one API manifest against the provided environment
// and returns the Findings it produces. An API may yield:
//
//   - a single Finding with StatusNoAuth when the manifest declares
//     auth type "none" or has no env vars
//   - one Finding per declared env var otherwise
//   - an additional StatusUnknown Finding when browser-session proof is
//     required; env var findings are still reported because they are useful
//     setup diagnostics
//   - a single Finding with StatusUnknown when the manifest is nil or malformed
//
// slug is the API identifier used in output. The manifest's own
// Auth.Type is reported verbatim.
func Classify(slug string, manifest *pipeline.ToolsManifest, env getEnv) []Finding {
	if manifest == nil {
		return []Finding{{
			API:    slug,
			Status: StatusUnknown,
			Reason: "manifest missing or unreadable",
		}}
	}

	findings := classifyAuthBlock(slug, manifest.Auth, env, "")
	if manifest.TierRouting != nil {
		tierNames := make([]string, 0, len(manifest.TierRouting.Tiers))
		for name := range manifest.TierRouting.Tiers {
			tierNames = append(tierNames, name)
		}
		sort.Strings(tierNames)
		for _, tierName := range tierNames {
			findings = append(findings, classifyAuthBlock(slug, manifest.TierRouting.Tiers[tierName].Auth, env, tierName)...)
		}
	}
	if len(findings) == 0 {
		return []Finding{{
			API:    slug,
			Type:   displayType(manifest.Auth.Type),
			Status: StatusNoAuth,
		}}
	}
	return findings
}

func classifyAuthBlock(slug string, auth pipeline.ManifestAuth, env getEnv, tierName string) []Finding {
	authType := auth.Type
	if authType == "" || authType == "none" {
		return nil
	}
	displayAuthType := scopedAuthType(tierName, authType)
	if len(auth.EnvVarSpecs) > 0 && len(auth.EnvVars) > 0 && !sameAuthEnvVarNames(auth.EnvVars, auth.EnvVarSpecs) {
		fmt.Fprintln(os.Stderr, "warning: tools-manifest auth env_vars disagree with env_var_specs; using env_var_specs")
	}
	envVarSpecs := auth.EffectiveEnvVarSpecs()
	if len(envVarSpecs) == 0 {
		findings := []Finding{{
			API:    slug,
			Type:   displayAuthType,
			Status: StatusUnknown,
			Reason: "auth type declared but no env_vars listed in manifest",
		}}
		if auth.RequiresBrowserSession {
			findings = append(findings, browserSessionProofFinding(slug, displayAuthType))
		}
		return findings
	}

	findings := make([]Finding, 0, len(envVarSpecs))
	harvestedAuthFileExists := false
	if hasHarvestedEnvVar(envVarSpecs) {
		harvestedAuthFileExists = authFileExists(slug)
	}
	for _, envVar := range envVarSpecs {
		findings = append(findings, classifyEnvSpec(slug, displayAuthType, authType, envVar, env, harvestedAuthFileExists))
	}
	if auth.RequiresBrowserSession {
		findings = append(findings, browserSessionProofFinding(slug, displayAuthType))
	}
	return findings
}

func hasHarvestedEnvVar(envVarSpecs []spec.AuthEnvVar) bool {
	for _, envVar := range envVarSpecs {
		if envVar.Kind == spec.AuthEnvVarKindHarvested {
			return true
		}
	}
	return false
}

func sameAuthEnvVarNames(envVars []string, envVarSpecs []spec.AuthEnvVar) bool {
	if len(envVars) != len(envVarSpecs) {
		return false
	}
	counts := make(map[string]int, len(envVars))
	for _, envVar := range envVars {
		counts[strings.TrimSpace(envVar)]++
	}
	for _, envVarSpec := range envVarSpecs {
		name := strings.TrimSpace(envVarSpec.Name)
		if counts[name] == 0 {
			return false
		}
		counts[name]--
	}
	return true
}

func scopedAuthType(tierName, authType string) string {
	if tierName == "" {
		return authType
	}
	return fmt.Sprintf("tier:%s/%s", tierName, authType)
}

func browserSessionProofFinding(slug, authType string) Finding {
	return Finding{
		API:    slug,
		Type:   authType,
		Status: StatusUnknown,
		Reason: "requires browser-session proof; run the printed CLI's doctor command",
	}
}

// classifyEnv builds one Finding for a single (api, auth-type, env-var) triple.
func classifyEnv(slug, authType, envVar string, env getEnv) Finding {
	value := env(envVar)
	base := Finding{
		API:    slug,
		Type:   authType,
		EnvVar: envVar,
	}

	if value == "" {
		base.Status = StatusNotSet
		return base
	}

	// Suspicious-value heuristics.
	if reason := suspiciousReason(authType, value); reason != "" {
		base.Status = StatusSuspicious
		base.Reason = reason
		base.Fingerprint = Fingerprint(value)
		return base
	}

	base.Status = StatusOK
	base.Fingerprint = Fingerprint(value)
	return base
}

func classifyEnvSpec(slug, displayAuthType, authType string, envVar spec.AuthEnvVar, env getEnv, harvestedAuthFileExists bool) Finding {
	kind := envVar.Kind
	if kind == "" {
		kind = spec.AuthEnvVarKindPerCall
	}
	switch kind {
	case spec.AuthEnvVarKindAuthFlowInput:
		return classifyInfoEnv(slug, displayAuthType, envVar.Name, env, "only needed during auth login")
	case spec.AuthEnvVarKindHarvested:
		if env(envVar.Name) != "" {
			return classifyEnv(slug, displayAuthType, envVar.Name, env)
		}
		if harvestedAuthFileExists {
			return Finding{
				API:    slug,
				Type:   displayAuthType,
				EnvVar: envVar.Name,
				Status: StatusOK,
				Reason: "auth file present",
			}
		}
		return Finding{
			API:    slug,
			Type:   displayAuthType,
			EnvVar: envVar.Name,
			Status: StatusInfo,
			Reason: harvestedAuthReason(authType),
		}
	default:
		if !envVar.Required && env(envVar.Name) == "" {
			return classifyInfoEnv(slug, displayAuthType, envVar.Name, env, "optional auth env var is not set")
		}
		return classifyEnv(slug, displayAuthType, envVar.Name, env)
	}
}

func harvestedAuthReason(authType string) string {
	switch authType {
	case "cookie", "composed":
		return "populated by auth login; run auth login --chrome"
	default:
		return "populated by auth login; run the printed CLI's auth command"
	}
}

func classifyInfoEnv(slug, authType, envVar string, env getEnv, reason string) Finding {
	if env(envVar) != "" {
		return classifyEnv(slug, authType, envVar, env)
	}
	return Finding{
		API:    slug,
		Type:   authType,
		EnvVar: envVar,
		Status: StatusInfo,
		Reason: reason,
	}
}

func authFileExists(slug string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	path := filepath.Join(home, ".config", slug+"-pp-cli", "config.toml")
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// suspiciousReason returns a non-empty reason when a set value looks
// obviously malformed. Returns empty when the value looks acceptable.
func suspiciousReason(authType, value string) string {
	// Leading or trailing whitespace is almost always a paste error.
	if trimmed := trimmedLen(value); trimmed != len(value) {
		return "value has surrounding whitespace"
	}

	minLen, ok := minLengthByType[authType]
	if !ok {
		// Unknown types are not length-gated.
		return ""
	}
	if len(value) < minLen {
		return fmt.Sprintf("value is %d chars, expected at least %d for %s", len(value), minLen, authType)
	}
	return ""
}

// trimmedLen returns the length of value after trimming ASCII spaces,
// tabs, newlines, and carriage returns. Used to detect paste errors.
func trimmedLen(value string) int {
	start, end := 0, len(value)
	for start < end && isSpace(value[start]) {
		start++
	}
	for end > start && isSpace(value[end-1]) {
		end--
	}
	return end - start
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// displayType returns a stable display string for the auth type field
// when the manifest's own value is empty. "none" and "" both render as
// "none" so the table is consistent.
func displayType(authType string) string {
	if authType == "" {
		return "none"
	}
	return authType
}
