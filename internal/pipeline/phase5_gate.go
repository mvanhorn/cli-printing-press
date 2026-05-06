package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	Phase5AcceptanceFilename = "phase5-acceptance.json"
	Phase5SkipFilename       = "phase5-skip.json"
)

type Phase5AuthContext struct {
	Type                    string `json:"type,omitempty"`
	APIKeyAvailable         bool   `json:"api_key_available,omitempty"`
	BrowserSessionAvailable bool   `json:"browser_session_available,omitempty"`
}

type Phase5GateMarker struct {
	SchemaVersion int               `json:"schema_version"`
	APIName       string            `json:"api_name,omitempty"`
	RunID         string            `json:"run_id,omitempty"`
	Status        string            `json:"status"`
	Level         string            `json:"level,omitempty"`
	MatrixSize    int               `json:"matrix_size,omitempty"`
	TestsPassed   int               `json:"tests_passed,omitempty"`
	TestsSkipped  int               `json:"tests_skipped,omitempty"`
	TestsFailed   int               `json:"tests_failed,omitempty"`
	AuthContext   Phase5AuthContext `json:"auth_context,omitzero"`
	SkipReason    string            `json:"skip_reason,omitempty"`
}

type Phase5GateValidation struct {
	Passed     bool
	Status     string
	MarkerPath string
	Detail     string
}

func ValidatePhase5Gate(proofsDir string, manifest CLIManifest) Phase5GateValidation {
	if strings.TrimSpace(proofsDir) == "" {
		return Phase5GateValidation{Detail: "phase5 proofs directory is empty"}
	}

	if result, ok := validatePhase5MarkerFile(filepath.Join(proofsDir, Phase5AcceptanceFilename), manifest, false); ok {
		return result
	}
	if result, ok := validatePhase5MarkerFile(filepath.Join(proofsDir, Phase5SkipFilename), manifest, true); ok {
		return result
	}

	return Phase5GateValidation{
		Detail: fmt.Sprintf("missing %s or %s in %s", Phase5AcceptanceFilename, Phase5SkipFilename, proofsDir),
	}
}

func validatePhase5MarkerFile(path string, manifest CLIManifest, skipFile bool) (Phase5GateValidation, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Phase5GateValidation{}, false
		}
		return Phase5GateValidation{MarkerPath: path, Detail: fmt.Sprintf("reading phase5 marker: %v", err)}, true
	}

	var marker Phase5GateMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return Phase5GateValidation{MarkerPath: path, Detail: fmt.Sprintf("parsing phase5 marker: %v", err)}, true
	}

	result := validatePhase5Marker(marker, manifest, skipFile)
	result.MarkerPath = path
	return result, true
}

func validatePhase5Marker(marker Phase5GateMarker, manifest CLIManifest, skipFile bool) Phase5GateValidation {
	status := strings.ToLower(strings.TrimSpace(marker.Status))
	result := Phase5GateValidation{Status: status}

	if marker.SchemaVersion != 1 {
		result.Detail = fmt.Sprintf("unsupported phase5 marker schema_version %d", marker.SchemaVersion)
		return result
	}
	if marker.APIName != "" && manifest.APIName != "" && marker.APIName != manifest.APIName {
		result.Detail = fmt.Sprintf("phase5 marker api_name %q does not match manifest api_name %q", marker.APIName, manifest.APIName)
		return result
	}
	if marker.RunID != "" && manifest.RunID != "" && marker.RunID != manifest.RunID {
		result.Detail = fmt.Sprintf("phase5 marker run_id %q does not match manifest run_id %q", marker.RunID, manifest.RunID)
		return result
	}

	switch status {
	case "pass":
		if skipFile {
			result.Detail = fmt.Sprintf("%s must use status skip, got pass", Phase5SkipFilename)
			return result
		}
		if detail := validatePhase5PassMarker(marker); detail != "" {
			result.Detail = detail
			return result
		}
		if ok, detail := phase5AcceptancePassed(marker); !ok {
			result.Detail = detail
			return result
		}
		result.Passed = true
		return result
	case "fail":
		result.Detail = "phase5 gate status is fail"
		return result
	case "skip":
		if !skipFile {
			result.Detail = fmt.Sprintf("%s must use status pass or fail, got skip", Phase5AcceptanceFilename)
			return result
		}
		if detail := validatePhase5SkipMarker(marker); detail != "" {
			result.Detail = detail
			return result
		}
		if ok, detail := phase5SkipAllowed(marker, manifest); !ok {
			result.Detail = detail
			return result
		}
		result.Passed = true
		return result
	default:
		result.Detail = fmt.Sprintf("unknown phase5 gate status %q", marker.Status)
		return result
	}
}

func validatePhase5PassMarker(marker Phase5GateMarker) string {
	switch {
	case strings.TrimSpace(marker.APIName) == "":
		return "phase5 acceptance marker missing api_name"
	case strings.TrimSpace(marker.RunID) == "":
		return "phase5 acceptance marker missing run_id"
	case phase5Level(marker) == "":
		return "phase5 acceptance marker missing level"
	case marker.MatrixSize <= 0:
		return "phase5 acceptance marker missing matrix_size"
	case marker.TestsPassed <= 0:
		return "phase5 acceptance marker missing tests_passed"
	default:
		return ""
	}
}

func phase5AcceptancePassed(marker Phase5GateMarker) (bool, string) {
	level := phase5Level(marker)
	switch level {
	case "quick":
		if marker.TestsFailed != 0 {
			return false, fmt.Sprintf("phase5 quick acceptance has %d failed tests", marker.TestsFailed)
		}
		if marker.TestsPassed != marker.MatrixSize {
			return false, fmt.Sprintf("phase5 quick acceptance requires all %d counted tests passed, got %d", marker.MatrixSize, marker.TestsPassed)
		}
		// Mirror finalizeLiveDogfoodReport's quick PASS condition:
		// MatrixSize >= 4 AND Passed+Skipped >= min(5, MatrixSize). The runner
		// is the source of truth; this gate must accept any marker the runner
		// would have accepted. Drift here was the original bug (#589/#590).
		if marker.MatrixSize < 4 {
			return false, fmt.Sprintf("phase5 quick acceptance requires matrix_size >= 4, got %d", marker.MatrixSize)
		}
		threshold := min(5, marker.MatrixSize)
		passOrSkip := marker.TestsPassed + marker.TestsSkipped
		if passOrSkip < threshold {
			return false, fmt.Sprintf("phase5 quick acceptance requires at least %d/%d tests passed-or-skipped, got %d", threshold, marker.MatrixSize, passOrSkip)
		}
		return true, ""
	case "full":
		if marker.TestsFailed != 0 {
			return false, fmt.Sprintf("phase5 full acceptance has %d failed tests", marker.TestsFailed)
		}
		if marker.TestsPassed != marker.MatrixSize {
			return false, fmt.Sprintf("phase5 full acceptance requires all %d tests passed, got %d", marker.MatrixSize, marker.TestsPassed)
		}
		return true, ""
	default:
		return false, fmt.Sprintf("unknown phase5 acceptance level %q", marker.Level)
	}
}

func phase5Level(marker Phase5GateMarker) string {
	return strings.ToLower(strings.TrimSpace(marker.Level))
}

func validatePhase5SkipMarker(marker Phase5GateMarker) string {
	switch {
	case strings.TrimSpace(marker.APIName) == "":
		return "phase5 skip marker missing api_name"
	case strings.TrimSpace(marker.RunID) == "":
		return "phase5 skip marker missing run_id"
	case strings.TrimSpace(marker.SkipReason) == "":
		return "phase5 skip marker missing skip_reason"
	default:
		return ""
	}
}

func phase5SkipAllowed(marker Phase5GateMarker, manifest CLIManifest) (bool, string) {
	authType := strings.ToLower(strings.TrimSpace(manifest.AuthType))
	markerAuthType := strings.ToLower(strings.TrimSpace(marker.AuthContext.Type))
	if authType == "" {
		authType = markerAuthType
	} else if markerAuthType != "" && markerAuthType != authType {
		return false, fmt.Sprintf("phase5 skip marker auth type %q does not match manifest auth type %q", marker.AuthContext.Type, manifest.AuthType)
	}
	if authType == "" || authType == "none" {
		return false, "no-auth APIs require a phase5 pass marker, not a skip marker"
	}
	if marker.AuthContext.APIKeyAvailable {
		return false, "phase5 skip claims an API key was available"
	}
	if authRequiresCredential(authType) {
		return true, ""
	}
	switch authType {
	case "cookie", "composed", "session_handshake":
		return false, "browser-session auth APIs require phase5 acceptance; missing API key is not a valid skip"
	default:
		return false, fmt.Sprintf("phase5 skip not allowed for auth type %q", authType)
	}
}
