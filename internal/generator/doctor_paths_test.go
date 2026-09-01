package generator

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedDoctorReportsPathsAndPathWarnings(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-paths")
	apiSpec.BaseURL = ""
	outputDir, binaryPath := buildGeneratedBinary(t, apiSpec)
	_ = outputDir

	home := t.TempDir()
	prefix := naming.EnvPrefix(apiSpec.Name)
	env := append(doctorEnv(home, prefix), "MYAPI_TOKEN=doctor-token")
	payload, err := runDoctorJSON(t, binaryPath, env, "--fail-on", "error")
	require.NoErrorf(t, err, "informational path lines must not trip --fail-on=error; payload=%v", payload)
	paths := requirePathsPayload(t, payload)
	for _, kind := range []string{"config", "data", "state", "cache"} {
		entry := requirePathEntry(t, paths, kind)
		require.Equal(t, "platform-default", entry["rung"])
		require.NotEmpty(t, entry["dir"])
	}
	require.NotContains(t, payload, "credentials_location_warning")

	dataDir := filepath.Join(t.TempDir(), "data")
	env = append(doctorEnv(home, prefix), prefix+"_DATA_DIR="+dataDir)
	payload, err = runDoctorJSON(t, binaryPath, env)
	require.NoError(t, err)
	dataEntry := requirePathEntry(t, requirePathsPayload(t, payload), "data")
	require.Equal(t, "per-kind-env", dataEntry["rung"])
	require.Equal(t, prefix+"_DATA_DIR", dataEntry["source"])
	require.Equal(t, dataDir, dataEntry["dir"])

	env = append(doctorEnv(home, prefix), "MYAPI_TOKEN=doctor-token", prefix+"_DATA_DIR=relative/data")
	payload, err = runDoctorJSON(t, binaryPath, env)
	require.NoError(t, err)
	skipped, ok := requirePathsPayload(t, payload)["skipped_relative_overrides"].([]any)
	require.True(t, ok, "relative override should be surfaced")
	require.NotEmpty(t, skipped)
	require.Contains(t, payload["paths_warning"], "WARN paths: relative override skipped: "+prefix+"_DATA_DIR")
	require.NotContains(t, payload["paths_warning"], "error")
	require.NotContains(t, payload["paths_warning"], "invalid")
	require.NotContains(t, payload["paths_warning"], "missing")
	require.NotContains(t, payload["paths_warning"], "unreachable")
	_, err = runDoctorJSON(t, binaryPath, env, "--fail-on", "warn")
	require.Error(t, err, "--fail-on=warn should trip on relative path override warnings")
	_, err = runDoctorJSON(t, binaryPath, env, "--fail-on", "error")
	require.NoError(t, err, "--fail-on=error should not trip on path WARN text")

	shadowedData := filepath.Join(t.TempDir(), "shadow-data")
	env = append(doctorEnv(home, prefix), "MYAPI_TOKEN=doctor-token", prefix+"_DATA_DIR="+shadowedData)
	payload, err = runDoctorJSON(t, binaryPath, env, "--home", filepath.Join(t.TempDir(), "flag-home"))
	require.NoError(t, err)
	notes, ok := requirePathsPayload(t, payload)["notes"].([]any)
	require.True(t, ok, "--home shadow note should be surfaced")
	require.Contains(t, notes[0].(string), prefix+"_DATA_DIR")
	require.Contains(t, payload["paths_warning"], "WARN paths: home override shadowed")
}

func TestGeneratedDoctorCredentialStateBWarnAndFailOnWarn(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-state-b")
	apiSpec.BaseURL = ""
	_, binaryPath := buildGeneratedBinary(t, apiSpec)
	home := t.TempDir()
	prefix := naming.EnvPrefix(apiSpec.Name)
	seedCredentialStateB(t, home, apiSpec.Name, "legacy-token", "new-token", false)

	payload, err := runDoctorJSON(t, binaryPath, doctorEnv(home, prefix))
	require.NoError(t, err)
	require.Contains(t, payload["credentials_location_warning"], "WARN credentials stored in more than one location")

	_, err = runDoctorJSON(t, binaryPath, doctorEnv(home, prefix), "--fail-on", "warn")
	require.Error(t, err, "--fail-on=warn should exit non-zero for state B")
	_, err = runDoctorJSON(t, binaryPath, doctorEnv(home, prefix), "--fail-on", "stale")
	require.NoError(t, err, "--fail-on=stale should not trip on a WARN-only state")
	_, err = runDoctorJSON(t, binaryPath, doctorEnv(home, prefix), "--fail-on", "error")
	require.NoError(t, err, "--fail-on=error should not trip on a WARN-only state")
}

func TestGeneratedDoctorAuthNoneOmitsCredentialLocations(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-auth-none-paths")
	apiSpec.BaseURL = ""
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	_, binaryPath := buildGeneratedBinary(t, apiSpec)

	payload, err := runDoctorJSON(t, binaryPath, doctorEnv(t.TempDir(), naming.EnvPrefix(apiSpec.Name)))
	require.NoError(t, err)
	require.Equal(t, "not required", payload["auth"])
	require.NotContains(t, payload, "credentials_location")
	require.NotContains(t, payload, "credentials_locations")
	require.NotContains(t, payload, "credentials_location_warning")
}

func TestGeneratedDoctorDistinguishesEmptyCacheFromFresh(t *testing.T) {
	t.Parallel()

	apiSpec := readOnlyCollectionSpec("doctor-empty-cache")
	apiSpec.Cache.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testSrc := strings.ReplaceAll(`package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectCacheReportEmptyStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	dbPath := defaultDBPath("__CLI_NAME__")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
		t.Fatalf("seed empty DB file: %v", err)
	}

	report := collectCacheReport(context.Background(), "")
	if got := report["status"]; got != "empty" {
		t.Fatalf("status = %v, want empty; report=%v", got, report)
	}
	if _, ok := report["fresh_resources"]; ok {
		t.Fatalf("empty store must not report fresh resources: %v", report)
	}
}
`, "__CLI_NAME__", naming.CLI(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "cache_empty_test.go"), []byte(testSrc), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestCollectCacheReportEmptyStore", "-count=1")
}

func TestGeneratedDoctorClassifiesSyncStateTimes(t *testing.T) {
	t.Parallel()

	apiSpec := readOnlyCollectionSpec("doctor-sync-state-times")
	apiSpec.Cache.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testSrc := strings.ReplaceAll(`package cli

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type syncStateRow struct {
	resourceType string
	lastSyncedAt any
}

func TestCollectCacheReportSyncStateTimes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	dbPath := defaultDBPath("__CLI_NAME__")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
		t.Fatalf("seed empty DB file: %v", err)
	}
	collectCacheReport(context.Background(), "")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	defer db.Close()

	zero := time.Time{}.UTC().Format(time.RFC3339)
	old := time.Now().UTC().Add(-12 * time.Hour).Format(time.RFC3339)
	tests := []struct {
		name       string
		rows       []syncStateRow
		wantStatus string
		wantOldest bool
	}{
		{
			name:       "null",
			rows:       []syncStateRow{{resourceType: "null-resource", lastSyncedAt: nil}},
			wantStatus: "empty",
		},
		{
			name:       "zero",
			rows:       []syncStateRow{{resourceType: "zero-resource", lastSyncedAt: zero}},
			wantStatus: "empty",
		},
		{
			name:       "real old",
			rows:       []syncStateRow{{resourceType: "old-resource", lastSyncedAt: old}},
			wantStatus: "stale",
			wantOldest: true,
		},
		{
			name: "mixed zero and real",
			rows: []syncStateRow{
				{resourceType: "zero-resource", lastSyncedAt: zero},
				{resourceType: "old-resource", lastSyncedAt: old},
			},
			wantStatus: "stale",
			wantOldest: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := db.Exec("DELETE FROM sync_state"); err != nil {
				t.Fatalf("clear sync_state: %v", err)
			}
			for _, row := range tt.rows {
				if _, err := db.Exec(
					"INSERT INTO sync_state(resource_type, last_synced_at, total_count) VALUES (?, ?, 0)",
					row.resourceType,
					row.lastSyncedAt,
				); err != nil {
					t.Fatalf("insert sync_state: %v", err)
				}
			}

			report := collectCacheReport(context.Background(), "6h")
			if got := report["status"]; got != tt.wantStatus {
				t.Fatalf("status = %v, want %q; report=%v", got, tt.wantStatus, report)
			}

			resources, ok := report["resources"].([]map[string]any)
			if !ok {
				t.Fatalf("resources has type %T, want []map[string]any", report["resources"])
			}
			for _, resource := range resources {
				if resource["type"] == "zero-resource" || resource["type"] == "null-resource" {
					if got := resource["staleness"]; got != "never" {
						t.Fatalf("%v staleness = %v, want never", resource["type"], got)
					}
					if _, ok := resource["last_synced_at"]; ok {
						t.Fatalf("%v must not render last_synced_at: %v", resource["type"], resource)
					}
				}
			}

			oldest, hasOldest := report["oldest_age"]
			if !tt.wantOldest {
				if hasOldest {
					t.Fatalf("oldest_age = %v, want omitted; report=%v", oldest, report)
				}
				return
			}
			if !hasOldest {
				t.Fatalf("oldest_age omitted; report=%v", report)
			}
			age, err := time.ParseDuration(oldest.(string))
			if err != nil {
				t.Fatalf("parse oldest_age %q: %v", oldest, err)
			}
			if age < 11*time.Hour || age > 13*time.Hour {
				t.Fatalf("oldest_age = %v, want age of the real timestamp only", age)
			}
		})
	}
}
`, "__CLI_NAME__", naming.CLI(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "cache_sync_state_test.go"), []byte(testSrc), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestCollectCacheReportSyncStateTimes", "-count=1")
}

func TestGeneratedDoctorAgentcookieSuppressesMultiLocationWarn(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-agentcookie-paths")
	apiSpec.BaseURL = ""
	_, binaryPath := buildGeneratedBinary(t, apiSpec)
	home := t.TempDir()
	prefix := naming.EnvPrefix(apiSpec.Name)
	seedCredentialStateB(t, home, apiSpec.Name, "legacy-token", "new-token", true)

	payload, err := runDoctorJSON(t, binaryPath, doctorEnv(home, prefix))
	require.NoError(t, err)
	require.Equal(t, "detected (managing credentials)", payload["agentcookie"])
	require.Equal(t, "agentcookie", payload["credentials_location"])
	require.NotContains(t, payload, "credentials_location_warning")
}

func buildGeneratedBinary(t *testing.T, apiSpec *spec.APISpec) (string, string) {
	t.Helper()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	runGoCommand(t, outputDir, "mod", "tidy")
	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	return outputDir, binaryPath
}

func runDoctorJSON(t *testing.T, binaryPath string, env []string, extraArgs ...string) (map[string]any, error) {
	t.Helper()
	args := append([]string{"doctor", "--json"}, extraArgs...)
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		if ee, ok := err.(*exec.ExitError); ok {
			out = ee.Stderr
			if len(out) == 0 {
				out = []byte(ee.String())
			}
		}
	}
	if len(out) == 0 {
		return nil, err
	}
	var payload map[string]any
	require.NoError(t, json.Unmarshal(out, &payload), "doctor --json output: %s", string(out))
	return payload, err
}

func doctorEnv(home, prefix string) []string {
	env := os.Environ()
	clears := []string{
		"HOME=" + home,
		prefix + "_CONFIG=",
		prefix + "_CONFIG_DIR=",
		prefix + "_DATA_DIR=",
		prefix + "_STATE_DIR=",
		prefix + "_CACHE_DIR=",
		prefix + "_HOME=",
		"XDG_CONFIG_HOME=",
		"XDG_DATA_HOME=",
		"XDG_STATE_HOME=",
		"XDG_CACHE_HOME=",
	}
	return append(env, clears...)
}

func seedCredentialStateB(t *testing.T, home, name, legacy, current string, agentcookie bool) {
	t.Helper()
	configDir := filepath.Join(home, ".config", name+"-pp-cli")
	dataDir := filepath.Join(home, ".local", "share", name+"-pp-cli")
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	require.NoError(t, os.MkdirAll(dataDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(`auth_header = "`+legacy+`"`+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "credentials.toml"), []byte(`auth_header = "`+current+`"`+"\n"), 0o600))
	if agentcookie {
		require.NoError(t, os.WriteFile(filepath.Join(configDir, ".agentcookie-managed"), []byte("managed\n"), 0o600))
	}
}

func requirePathsPayload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	paths, ok := payload["paths"].(map[string]any)
	require.True(t, ok, "doctor payload must include paths")
	return paths
}

func requirePathEntry(t *testing.T, paths map[string]any, kind string) map[string]any {
	t.Helper()
	entry, ok := paths[kind].(map[string]any)
	require.True(t, ok, "doctor paths.%s missing; keys: %s", kind, strings.Join(mapKeys(paths), ","))
	return entry
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestGeneratedDoctorCredentialLocationLabels pins the credentials_location
// label for config-stored credentials: the label must report the file the
// credentials were actually parsed from. Relocated config-kind paths and
// explicit --config files are "config-kind path"; only a read that fell
// back to the pre-paths layout reports "legacy config path".
func TestGeneratedDoctorCredentialLocationLabels(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-cred-labels")
	apiSpec.BaseURL = ""
	_, binaryPath := buildGeneratedBinary(t, apiSpec)
	prefix := naming.EnvPrefix(apiSpec.Name)

	// Relocated config-kind path via per-kind env var.
	relocatedDir := filepath.Join(t.TempDir(), "relocated-config")
	require.NoError(t, os.MkdirAll(relocatedDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(relocatedDir, "config.toml"), []byte("token = \"relocated-secret\"\n"), 0o600))
	env := append(doctorEnv(t.TempDir(), prefix), "MYAPI_TOKEN=", prefix+"_CONFIG_DIR="+relocatedDir)
	payload, err := runDoctorJSON(t, binaryPath, env)
	require.NoError(t, err)
	require.Equal(t, "config-kind path", payload["credentials_location"],
		"relocated config credentials must not be labeled legacy")

	// Explicit --config file.
	explicitPath := filepath.Join(t.TempDir(), "explicit.toml")
	require.NoError(t, os.WriteFile(explicitPath, []byte("token = \"explicit-secret\"\n"), 0o600))
	env = append(doctorEnv(t.TempDir(), prefix), "MYAPI_TOKEN=")
	payload, err = runDoctorJSON(t, binaryPath, env, "--config", explicitPath)
	require.NoError(t, err)
	require.Equal(t, "config-kind path", payload["credentials_location"],
		"explicit --config credentials must not be labeled legacy")

	// Legacy fallback: the pre-paths location keeps the legacy label.
	legacyHome := t.TempDir()
	legacyDir := filepath.Join(legacyHome, ".config", apiSpec.Name+"-pp-cli")
	require.NoError(t, os.MkdirAll(legacyDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "config.toml"), []byte("token = \"legacy-secret\"\n"), 0o600))
	env = append(doctorEnv(legacyHome, prefix), "MYAPI_TOKEN=")
	payload, err = runDoctorJSON(t, binaryPath, env)
	require.NoError(t, err)
	require.Equal(t, "legacy config path", payload["credentials_location"])
}

func TestGeneratedDoctorHumanOutputShowsCollectedFailureReasons(t *testing.T) {
	t.Parallel()

	apiSpec := readOnlyCollectionSpec("doctor-render-errors")
	apiSpec.Cache.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctor := string(doctorSrc)
	require.Contains(t, doctor, `if v, ok := rep["error"]; ok {`,
		"renderCacheReport must print the collected cache error")
	require.Contains(t, doctor, `fmt.Fprintf(w, "    error: %v\n", v)`,
		"cache human output must include the collected error string")
	require.Contains(t, doctor, `if status, _ := rep["status"].(string); status == "error" {`,
		"renderPathsReport must surface a path-resolution failure")
	require.Contains(t, doctor, `fmt.Fprintf(w, "    detail: %v\n", v)`,
		"paths human output must include the collected detail string")

	testSrc := `package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderCacheReportPrintsError(t *testing.T) {
	var buf bytes.Buffer
	renderCacheReport(&buf, map[string]any{
		"status":  "error",
		"db_path": "/tmp/data.db",
		"error":   "running migrations: database schema version 9 is newer than supported version 4; upgrade the CLI binary or open an older database",
	})
	out := buf.String()
	if !strings.Contains(out, "FAIL") {
		t.Fatalf("cache human output missing FAIL: %q", out)
	}
	if !strings.Contains(out, "running migrations: database schema version 9") {
		t.Fatalf("cache human output discarded collected error: %q", out)
	}
}

func TestRenderPathsReportPrintsDetail(t *testing.T) {
	var buf bytes.Buffer
	renderPathsReport(&buf, map[string]any{
		"status": "error",
		"detail": "HOME is unset and no platform default is available",
	})
	out := buf.String()
	if !strings.Contains(out, "FAIL") {
		t.Fatalf("paths human output missing FAIL: %q", out)
	}
	if !strings.Contains(out, "HOME is unset and no platform default is available") {
		t.Fatalf("paths human output discarded collected detail: %q", out)
	}
	if strings.TrimSpace(out) == "Paths:" {
		t.Fatalf("paths failure rendered as a bare header")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "doctor_render_error_test.go"), []byte(testSrc), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestRender(Cache|Paths)Report", "-count=1")
}
