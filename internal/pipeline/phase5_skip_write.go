package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Phase5SkipWriteOptions selects the CLI tree and the skip marker to write.
type Phase5SkipWriteOptions struct {
	CLIDir  string
	Path    string
	Reason  string
	AuthEnv string
}

// WritePhase5SkipMarker writes phase5-skip.json with the source fingerprint
// lock promote and publish validate recompute. It refuses a skip either gate
// would reject, and a no-credential skip when a credential env var is set.
func WritePhase5SkipMarker(opts Phase5SkipWriteOptions) (Phase5GateMarker, error) {
	cliDir := strings.TrimSpace(opts.CLIDir)
	if cliDir == "" {
		return Phase5GateMarker{}, fmt.Errorf("CLI directory is empty")
	}
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return Phase5GateMarker{}, fmt.Errorf("phase5 skip path is empty")
	}
	if filepath.Base(path) != Phase5SkipFilename {
		return Phase5GateMarker{}, fmt.Errorf("phase5 skip path must be named %s, got %q", Phase5SkipFilename, filepath.Base(path))
	}
	reason := canonicalPhase5SkipReason(opts.Reason)
	if reason == "" {
		return Phase5GateMarker{}, fmt.Errorf("phase5 skip reason is empty")
	}
	// The live runner writes this only after it observes a session-less 401
	// cascade. Declaring it here would skip that matrix.
	if reason == phase5SkipReasonCookieAuthNoHarnessSession {
		return Phase5GateMarker{}, fmt.Errorf("phase5 skip reason %q is written by dogfood --live when no browser session is available; do not declare it with --write-skip", reason)
	}

	manifest, err := ReadCLIManifest(cliDir)
	if err != nil {
		return Phase5GateMarker{}, fmt.Errorf("reading CLI manifest: %w", err)
	}
	apiName, runID, authType := resolveLiveDogfoodAcceptanceIdentity(cliDir)
	if name := strings.TrimSpace(manifest.APIName); name != "" {
		apiName = name
	}
	if id := strings.TrimSpace(manifest.RunID); id != "" {
		runID = id
	}
	if kind := strings.TrimSpace(manifest.AuthType); kind != "" {
		authType = kind
	}
	if authType == "" {
		authType = "none"
	}
	manifest.APIName = apiName
	manifest.RunID = runID
	manifest.AuthType = authType

	source, err := CaptureSourceFingerprint(cliDir)
	if err != nil {
		return Phase5GateMarker{}, err
	}
	marker := Phase5GateMarker{
		SchemaVersion:     1,
		APIName:           apiName,
		RunID:             runID,
		Status:            "skip",
		Level:             "none",
		SkipReason:        reason,
		SourceFingerprint: source.Digest,
		SourceFiles:       source.Files,
		AuthContext:       phase5SkipAuthContext(authType, reason),
	}

	// Promote rebuilds a thinner manifest (no spec_format). A marker must
	// pass that view and the on-disk manifest publish validate uses.
	if detail := phase5SkipRejected(marker, []CLIManifest{manifest, phase5PromoteManifestView(manifest)}, cliDir); detail != "" {
		return Phase5GateMarker{}, fmt.Errorf("phase5 skip refused: %s", detail)
	}
	if name, present := phase5SkipCredentialPresent(manifest, opts.AuthEnv); present {
		claimed := marker
		claimed.AuthContext.APIKeyAvailable = true
		if ok, _ := phase5SkipAllowed(claimed, manifest); !ok {
			return Phase5GateMarker{}, fmt.Errorf("phase5 skip refused: credential env %s is set", name)
		}
	}

	if err := writeLiveDogfoodAcceptanceAndMirror(LiveDogfoodOptions{CLIDir: cliDir}, path, marker); err != nil {
		return Phase5GateMarker{}, err
	}
	// The gate reads phase5-acceptance.json first and ignores a sibling skip,
	// including a fail marker from an earlier live run.
	if err := removeShadowedPhase5Acceptance(path); err != nil {
		return Phase5GateMarker{}, err
	}
	if state, err := FindStateByWorkingDir(cliDir); err == nil && state != nil && strings.TrimSpace(state.RunID) != "" {
		mirrored := filepath.Join(state.ProofsDir(), Phase5SkipFilename)
		if !sameResolvedPath(path, mirrored) {
			if err := removeShadowedPhase5Acceptance(mirrored); err != nil {
				return Phase5GateMarker{}, err
			}
		}
	}
	return marker, nil
}

func canonicalPhase5SkipReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	switch strings.ToLower(trimmed) {
	case phase5SkipReasonAuthRequiredNoCredential:
		return phase5SkipReasonAuthRequiredNoCredential
	case phase5SkipReasonExternalCredentialsUnavailable:
		return phase5SkipReasonExternalCredentialsUnavailable
	case phase5SkipReasonLANUnreachableFromHost:
		return phase5SkipReasonLANUnreachableFromHost
	case phase5SkipReasonLocalSourceRequiresDatabase:
		return phase5SkipReasonLocalSourceRequiresDatabase
	case phase5SkipReasonCookieAuthNoHarnessSession:
		return phase5SkipReasonCookieAuthNoHarnessSession
	default:
		return trimmed
	}
}

func phase5SkipAuthContext(authType, reason string) Phase5AuthContext {
	ctx := Phase5AuthContext{Type: authType}
	if reason == phase5SkipReasonLANUnreachableFromHost {
		ctx.LocalNetworkOnly = true
	}
	if reason == phase5SkipReasonLocalSourceRequiresDatabase {
		ctx.LocalSQLite = true
	}
	return ctx
}

// phase5PromoteManifestView is the identity validatePhase5GateForPromote
// compares against. Spec format is not copied there, so a skip that depends
// on it cannot be promoted.
func phase5PromoteManifestView(manifest CLIManifest) CLIManifest {
	return CLIManifest{
		APIName:  manifest.APIName,
		CLIName:  manifest.CLIName,
		RunID:    manifest.RunID,
		AuthType: manifest.AuthType,
		SpecKind: manifest.SpecKind,
	}
}

func phase5SkipRejected(marker Phase5GateMarker, manifests []CLIManifest, sourceDir string) string {
	var details []string
	for _, manifest := range manifests {
		result := validatePhase5Marker(marker, manifest, true, sourceDir)
		if !result.Passed && result.Detail != "" {
			details = append(details, result.Detail)
		}
	}
	return strings.Join(uniqueStrings(details), "; ")
}

func phase5SkipCredentialPresent(manifest CLIManifest, named string) (string, bool) {
	for _, name := range phase5SkipCredentialEnvNames(manifest, named) {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return name, true
		}
	}
	return "", false
}

func phase5SkipCredentialEnvNames(manifest CLIManifest, named string) []string {
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || slices.Contains(names, name) {
			return
		}
		names = append(names, name)
	}
	add(named)
	for _, name := range manifest.AuthEnvVars {
		add(name)
	}
	for _, spec := range manifest.AuthEnvVarSpecs {
		add(spec.Name)
	}
	return names
}

func removeShadowedPhase5Acceptance(skipPath string) error {
	acceptance := filepath.Join(filepath.Dir(skipPath), Phase5AcceptanceFilename)
	if err := os.Remove(acceptance); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing shadowed %s: %w", Phase5AcceptanceFilename, err)
	}
	return nil
}
