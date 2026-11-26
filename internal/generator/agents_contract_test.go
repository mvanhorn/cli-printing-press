package generator

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedAgentsContractIsolatesOperatorHome(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("agents-iso")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.BaseURL = ""
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliName := naming.CLI(apiSpec.Name)
	prefix := naming.EnvPrefix(apiSpec.Name)
	agents := readFileString(t, filepath.Join(outputDir, "AGENTS.md"))
	assertHTTPAgentsIsolation(t, agents, cliName, prefix, true)
	assertAgentsSkillOmitsSourceContract(t, outputDir)
	assertPrintedPathEnv(t, outputDir, prefix)
	writeLegacyConfigProbe(t, outputDir, cliName, prefix, apiSpec.Config.Format, "", false)

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "-count=1", "./internal/cliutil/", "-run", "TestKindDirHomeEnvUsesFlatKindLayout|TestKindDirPerKindEnvBeatsHomeEnv")
	runGoCommand(t, outputDir, "test", "-count=1", "./internal/config/", "-run", "TestPrintedConfigEnvSkipsLegacyHome")

	binaryPath := platform.ExecutablePath(filepath.Join(outputDir, cliName))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+cliName)

	sentinel := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sentinel, "marker"), []byte("x"), 0o644))
	iso := t.TempDir()
	xdg := t.TempDir()
	env := append(doctorEnv(sentinel, prefix),
		prefix+"_HOME="+iso,
		"XDG_CONFIG_HOME="+xdg,
		"XDG_DATA_HOME="+xdg,
		"XDG_STATE_HOME="+xdg,
		"XDG_CACHE_HOME="+xdg,
	)
	payload, err := runDoctorJSON(t, binaryPath, env)
	require.NoError(t, err)
	for _, kind := range []string{"config", "data", "state", "cache"} {
		entry := requirePathEntry(t, requirePathsPayload(t, payload), kind)
		require.Equal(t, "home-env", entry["rung"])
		require.Equal(t, prefix+"_HOME", entry["source"])
		require.Equal(t, filepath.Join(iso, kind), entry["dir"])
	}
	require.Equal(t, []string{"marker"}, walkRel(t, sentinel))

	ctxOut, err := runBinary(t, binaryPath, env, "agent-context", "--pretty")
	require.NoError(t, err)
	require.True(t, json.Valid(ctxOut), "agent-context output: %s", ctxOut)

	t.Run("per-kind beats home env", func(t *testing.T) {
		dataDir := t.TempDir()
		perKind := append(doctorEnv(sentinel, prefix), prefix+"_HOME="+iso, prefix+"_DATA_DIR="+dataDir)
		payload, err := runDoctorJSON(t, binaryPath, perKind)
		require.NoError(t, err)
		paths := requirePathsPayload(t, payload)
		data := requirePathEntry(t, paths, "data")
		require.Equal(t, "per-kind-env", data["rung"])
		require.Equal(t, prefix+"_DATA_DIR", data["source"])
		require.Equal(t, dataDir, data["dir"])
		config := requirePathEntry(t, paths, "config")
		require.Equal(t, "home-env", config["rung"])
		require.Equal(t, filepath.Join(iso, "config"), config["dir"])
	})

	t.Run("home flag relocates one invocation", func(t *testing.T) {
		flagHome := t.TempDir()
		payload, err := runDoctorJSON(t, binaryPath, doctorEnv(sentinel, prefix), "--home", flagHome)
		require.NoError(t, err)
		entry := requirePathEntry(t, requirePathsPayload(t, payload), "data")
		require.Equal(t, "--home", entry["rung"])
		require.Equal(t, filepath.Join(flagHome, "data"), entry["dir"])
	})

	t.Run("relative home env is skipped", func(t *testing.T) {
		relEnv := append(doctorEnv(sentinel, prefix), prefix+"_HOME="+"relative/home")
		payload, err := runDoctorJSON(t, binaryPath, relEnv)
		require.NoError(t, err)
		entry := requirePathEntry(t, requirePathsPayload(t, payload), "data")
		require.Equal(t, "platform-default", entry["rung"])
		require.NotEqual(t, "home-env", entry["rung"])
		dir, _ := entry["dir"].(string)
		require.True(t, strings.HasPrefix(dir, sentinel), "dir %s", dir)
		warning, _ := payload["paths_warning"].(string)
		require.Contains(t, warning, prefix+"_HOME")
	})

	t.Run("xdg beats bare home", func(t *testing.T) {
		bareHome := t.TempDir()
		xdgData := t.TempDir()
		env := append(doctorEnv(bareHome, prefix), "XDG_DATA_HOME="+xdgData)
		payload, err := runDoctorJSON(t, binaryPath, env)
		require.NoError(t, err)
		entry := requirePathEntry(t, requirePathsPayload(t, payload), "data")
		require.Equal(t, "xdg-env", entry["rung"])
		require.Equal(t, "XDG_DATA_HOME", entry["source"])
		require.Equal(t, filepath.Join(xdgData, cliName), entry["dir"])
	})
}

func TestGeneratedAgentsContractLearnDisabledOmitsLearnCommands(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("agents-iso-off")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.BaseURL = ""
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliName := naming.CLI(apiSpec.Name)
	prefix := naming.EnvPrefix(apiSpec.Name)
	agents := readFileString(t, filepath.Join(outputDir, "AGENTS.md"))
	assertHTTPAgentsIsolation(t, agents, cliName, prefix, false)
	require.NotContains(t, agents, "recall")
	require.NotContains(t, agents, "teach")
	assertAgentsSkillOmitsSourceContract(t, outputDir)
	assertPrintedPathEnv(t, outputDir, prefix)
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedAgentsConfigEnvSkipsLegacyHome(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("agents-cfg")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliName := naming.CLI(apiSpec.Name)
	prefix := naming.EnvPrefix(apiSpec.Name)
	agents := readFileString(t, filepath.Join(outputDir, "AGENTS.md"))
	require.Contains(t, agents, `export `+prefix+`_CONFIG="$iso/config.`+apiSpec.Config.Format+`"`)
	require.Contains(t, readFileString(t, filepath.Join(outputDir, "internal", "config", "config.go")), `os.Getenv("`+prefix+`_CONFIG")`)
	writeLegacyConfigProbe(t, outputDir, cliName, prefix, apiSpec.Config.Format, apiSpec.BaseURL, true)
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "-count=1", "./internal/config/", "-run", "TestPrintedConfigEnvSkipsLegacyHome")
}

func TestGeneratedDeviceAgentsContractNoLocalCache(t *testing.T) {
	t.Parallel()

	ds := parseDeviceFixture(t, "ble-minimal.yaml")
	outputDir := generateDevice(t, ds)
	agents := readFileString(t, filepath.Join(outputDir, "AGENTS.md"))
	assertDeviceAgentsBase(t, agents, naming.CLI(ds.Name))
	require.NotContains(t, agents, "os.UserCacheDir")
	require.NotContains(t, agents, "XDG_CACHE_HOME")
	require.NotContains(t, agents, "LocalAppData")
	require.NotContains(t, agents, "--store")
	require.NotContains(t, agents, "_HOME")
	root := readFileString(t, filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NotContains(t, root, "storePath")
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "device", "store.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "device", "session.go"))
	assertAgentsSkillOmitsSourceContract(t, outputDir)
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedDeviceAgentsContractStoreAndSession(t *testing.T) {
	t.Parallel()

	ds := parseDeviceFixture(t, "ble-session-telemetry.yaml")
	outputDir := generateDevice(t, ds)
	cliName := naming.CLI(ds.Name)
	agents := readFileString(t, filepath.Join(outputDir, "AGENTS.md"))
	assertDeviceAgentsBase(t, agents, cliName)
	require.Contains(t, agents, "Telemetry and session files are created under `os.UserCacheDir`")
	assertDeviceCacheProse(t, agents)
	require.Contains(t, agents, "--store <file>")
	require.Contains(t, agents, "The session runtime directory still follows `os.UserCacheDir`.")
	require.Contains(t, readFileString(t, filepath.Join(outputDir, "internal", "device", "store.go")), "os.UserCacheDir()")
	require.Contains(t, readFileString(t, filepath.Join(outputDir, "internal", "device", "session.go")), "os.UserCacheDir()")
	require.Contains(t, readFileString(t, filepath.Join(outputDir, "internal", "cli", "root.go")), `"store"`)
	assertAgentsSkillOmitsSourceContract(t, outputDir)
	writeDeviceCacheProbe(t, outputDir, cliName, true, true)
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "-count=1", "./internal/device/", "-run", "TestPrintedCacheFollowsUserCacheDir")
}

func TestGeneratedDeviceAgentsContractStoreOnly(t *testing.T) {
	t.Parallel()

	ds := parseDeviceFixture(t, "ble-session-telemetry.yaml")
	ds.Session.Mode = devicespec.SessionModeOneShot
	ds.Session.OneShotFallback = false
	outputDir := generateDevice(t, ds)
	cliName := naming.CLI(ds.Name)
	agents := readFileString(t, filepath.Join(outputDir, "AGENTS.md"))
	assertDeviceAgentsBase(t, agents, cliName)
	require.Contains(t, agents, "Telemetry files are created under `os.UserCacheDir`")
	require.NotContains(t, agents, "Telemetry and session files")
	require.NotContains(t, agents, "session runtime directory")
	require.Contains(t, agents, "--store <file>")
	assertDeviceCacheProse(t, agents)
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "device", "session.go"))
	require.Contains(t, readFileString(t, filepath.Join(outputDir, "internal", "device", "store.go")), "os.UserCacheDir()")
	writeDeviceCacheProbe(t, outputDir, cliName, true, false)
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "-count=1", "./internal/device/", "-run", "TestPrintedCacheFollowsUserCacheDir")
}

func TestGeneratedDeviceAgentsContractSessionOnly(t *testing.T) {
	t.Parallel()

	ds := parseDeviceFixture(t, "ble-minimal.yaml")
	ds.Session.Mode = devicespec.SessionModeRequired
	outputDir := generateDevice(t, ds)
	cliName := naming.CLI(ds.Name)
	agents := readFileString(t, filepath.Join(outputDir, "AGENTS.md"))
	assertDeviceAgentsBase(t, agents, cliName)
	require.Contains(t, agents, "Session files are created under `os.UserCacheDir`")
	require.NotContains(t, agents, "--store")
	require.NotContains(t, agents, "Telemetry files")
	require.NotContains(t, agents, "Telemetry and session")
	assertDeviceCacheProse(t, agents)
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "device", "store.go"))
	require.Contains(t, readFileString(t, filepath.Join(outputDir, "internal", "device", "session.go")), "os.UserCacheDir()")
	root := readFileString(t, filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NotContains(t, root, "storePath")
	writeDeviceCacheProbe(t, outputDir, cliName, false, true)
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "-count=1", "./internal/device/", "-run", "TestPrintedCacheFollowsUserCacheDir")
}

func assertHTTPAgentsIsolation(t *testing.T, agents, cliName, prefix string, learn bool) {
	t.Helper()
	require.Contains(t, agents, "Leave any installed `"+cliName+"` on `PATH` unused.")
	require.Contains(t, agents, "go build -o ./"+cliName+" ./cmd/"+cliName)
	require.Contains(t, agents, "go test ./...")
	require.Contains(t, agents, "./"+cliName+" doctor --json")
	require.Contains(t, agents, "./"+cliName+" agent-context --pretty")
	require.Contains(t, agents, "./"+cliName+" which \"<capability>\" --json")
	require.Contains(t, agents, "./"+cliName+" <command> --help")
	require.Contains(t, agents, "./"+cliName+" <command> --agent")
	require.Contains(t, agents, "./"+cliName+" <command> --dry-run --agent")
	require.Contains(t, agents, "`--agent` does not imply `--yes`")
	require.Contains(t, agents, "`\"dry_run\": true`")
	require.NotContains(t, agents, "\n"+cliName+" doctor --json\n")
	require.NotContains(t, agents, "Start by asking the generated CLI")
	require.NotContains(t, agents, "export HOME=")
	require.Contains(t, agents, prefix+"_HOME")
	require.Contains(t, agents, `export `+prefix+`_CONFIG="$iso/config.`)
	for _, suffix := range []string{"CONFIG_DIR", "DATA_DIR", "STATE_DIR", "CACHE_DIR"} {
		require.Contains(t, agents, prefix+"_"+suffix)
	}
	require.Contains(t, agents, "--home")
	require.Contains(t, agents, "a relative value is skipped")
	require.Contains(t, agents, `iso="$(mktemp -d)"`)
	require.Contains(t, agents, "on Windows, pass an empty absolute path")
	if learn {
		require.Contains(t, agents, "including `which` and `--help`, `recall`, and `teach`")
		require.Contains(t, agents, "Run these on the isolated `./"+cliName+"`")
		require.Contains(t, agents, "./"+cliName+" recall")
		require.Contains(t, agents, "./"+cliName+" teach")
		return
	}
	require.Contains(t, agents, "including `which` and `--help`. Send live API requests only when the task requires them.")
}

func assertDeviceAgentsBase(t *testing.T, agents, cliName string) {
	t.Helper()
	require.Contains(t, agents, "Leave any installed `"+cliName+"` on `PATH` unused.")
	require.Contains(t, agents, "go build -o ./"+cliName+" ./cmd/"+cliName)
	require.Contains(t, agents, "./"+cliName+" doctor --json")
	require.Contains(t, agents, "./"+cliName+" capabilities --json")
	require.Contains(t, agents, "do not open the user cache")
	require.Contains(t, agents, "do not actuate the device")
	require.Contains(t, agents, "replay-backed by default")
	require.Contains(t, agents, "--confirm-physical-effect")
	require.Contains(t, agents, "-tags ble_live")
	require.Contains(t, agents, "./"+cliName+" status --live --json")
	require.Contains(t, agents, "./"+cliName+" <command> --dry-run --json")
	require.NotContains(t, agents, "\n"+cliName+" doctor --json\n")
	require.NotContains(t, agents, "\n"+cliName+" capabilities --json\n")
	require.NotContains(t, agents, "Start by asking the generated CLI")
	require.NotContains(t, agents, "agent-context")
	require.NotContains(t, agents, "safe to run anywhere")
}

func assertDeviceCacheProse(t *testing.T, agents string) {
	t.Helper()
	require.Contains(t, agents, "os.UserCacheDir")
	require.Contains(t, agents, "$XDG_CACHE_HOME")
	require.Contains(t, agents, "$HOME/.cache")
	require.Contains(t, agents, "$HOME/Library/Caches")
	require.Contains(t, agents, "%LocalAppData%")
}

func assertAgentsSkillOmitsSourceContract(t *testing.T, outputDir string) {
	t.Helper()
	skill := readFileString(t, filepath.Join(outputDir, "SKILL.md"))
	require.NotContains(t, skill, "Leave any installed")
	require.NotContains(t, skill, "This file is for agents changing this CLI's source.")
}

func assertPrintedPathEnv(t *testing.T, outputDir, prefix string) {
	t.Helper()
	paths := readFileString(t, filepath.Join(outputDir, "internal", "cliutil", "paths.go"))
	require.Contains(t, paths, `const envPrefix = "`+prefix+`"`)
	require.Contains(t, paths, `envPrefix + "_HOME"`)
	for _, suffix := range []string{"CONFIG_DIR", "DATA_DIR", "STATE_DIR", "CACHE_DIR"} {
		require.Contains(t, paths, `"`+suffix+`"`)
	}
	root := readFileString(t, filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.Contains(t, root, `StringVar(&flags.homePath, "home"`)
}

func parseDeviceFixture(t *testing.T, name string) *devicespec.DeviceSpec {
	t.Helper()
	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", name))
	require.NoError(t, err)
	return ds
}

func generateDevice(t *testing.T, ds *devicespec.DeviceSpec) string {
	t.Helper()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(ds.Name))
	require.NoError(t, NewDevice(ds, outputDir).Generate())
	return outputDir
}

func runBinary(t *testing.T, binary string, env []string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	return cmd.Output()
}

func walkRel(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, rel)
		return nil
	})
	require.NoError(t, err)
	return out
}

func writeDeviceCacheProbe(t *testing.T, outputDir, cliName string, store, session bool) {
	t.Helper()
	var body strings.Builder
	body.WriteString("package device\n\nimport (\n")
	if session {
		body.WriteString("\t\"context\"\n")
	}
	body.WriteString("\t\"path/filepath\"\n\t\"runtime\"\n\t\"testing\"\n)\n\n")
	body.WriteString("func TestPrintedCacheFollowsUserCacheDir(t *testing.T) {\n")
	body.WriteString("\txdg := t.TempDir()\n\thome := t.TempDir()\n\tlocalAppData := t.TempDir()\n")
	body.WriteString("\tprimary := setUserCacheEnv(t, xdg, home, localAppData, true)\n")
	body.WriteString("\tavoid := home\n")
	body.WriteString("\tif runtime.GOOS == \"darwin\" || runtime.GOOS == \"ios\" || runtime.GOOS == \"plan9\" {\n\t\tavoid = xdg\n\t}\n")
	if store {
		body.WriteString("\tassertStorePath(t, primary, avoid)\n")
	}
	if session {
		body.WriteString("\tassertSessionDir(t, primary, avoid)\n")
	}
	body.WriteString("\tif runtime.GOOS == \"windows\" || runtime.GOOS == \"darwin\" || runtime.GOOS == \"ios\" || runtime.GOOS == \"plan9\" {\n\t\treturn\n\t}\n")
	body.WriteString("\tfallback := setUserCacheEnv(t, xdg, home, localAppData, false)\n")
	if store {
		body.WriteString("\tassertStorePath(t, fallback, xdg)\n")
	}
	if session {
		body.WriteString("\tassertSessionDir(t, fallback, xdg)\n")
	}
	body.WriteString("}\n\n")
	body.WriteString("func setUserCacheEnv(t *testing.T, xdg, home, localAppData string, xdgSet bool) string {\n")
	body.WriteString("\tt.Helper()\n\tswitch runtime.GOOS {\n")
	body.WriteString("\tcase \"windows\":\n\t\tt.Setenv(\"LocalAppData\", localAppData)\n\t\tt.Setenv(\"HOME\", home)\n\t\tt.Setenv(\"USERPROFILE\", home)\n\t\treturn localAppData\n")
	body.WriteString("\tcase \"darwin\", \"ios\":\n\t\tt.Setenv(\"HOME\", home)\n\t\tt.Setenv(\"XDG_CACHE_HOME\", xdg)\n\t\treturn filepath.Join(home, \"Library\", \"Caches\")\n")
	body.WriteString("\tcase \"plan9\":\n\t\tt.Setenv(\"home\", home)\n\t\treturn filepath.Join(home, \"lib\", \"cache\")\n")
	body.WriteString("\tdefault:\n\t\tt.Setenv(\"HOME\", home)\n\t\tt.Setenv(\"USERPROFILE\", home)\n")
	body.WriteString("\t\tif xdgSet {\n\t\t\tt.Setenv(\"XDG_CACHE_HOME\", xdg)\n\t\t\treturn xdg\n\t\t}\n")
	body.WriteString("\t\tt.Setenv(\"XDG_CACHE_HOME\", \"\")\n\t\treturn filepath.Join(home, \".cache\")\n\t}\n}\n\n")
	if store {
		body.WriteString("func assertStorePath(t *testing.T, root, avoid string) {\n")
		body.WriteString("\tt.Helper()\n")
		body.WriteString("\tgot, err := DefaultTelemetryStorePath(" + strconv.Quote(cliName) + ")\n")
		body.WriteString("\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n")
		body.WriteString("\twant := filepath.Join(root, " + strconv.Quote(cliName) + ", \"telemetry.jsonl\")\n")
		body.WriteString("\tif got != want {\n\t\tt.Fatalf(\"store path = %s, want %s\", got, want)\n\t}\n")
		body.WriteString("\tif pathWithin(got, avoid) {\n\t\tt.Fatalf(\"store path %s is under %s\", got, avoid)\n\t}\n}\n\n")
	}
	if session {
		body.WriteString("func assertSessionDir(t *testing.T, root, avoid string) {\n")
		body.WriteString("\tt.Helper()\n")
		body.WriteString("\tstatus, err := NewReplaySession().Start(context.Background())\n")
		body.WriteString("\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n")
		body.WriteString("\twant := filepath.Join(root, " + strconv.Quote(cliName) + ", \"session\")\n")
		body.WriteString("\tif status.RuntimeDir != want {\n\t\tt.Fatalf(\"session dir = %s, want %s\", status.RuntimeDir, want)\n\t}\n")
		body.WriteString("\tif pathWithin(status.RuntimeDir, avoid) {\n\t\tt.Fatalf(\"session dir %s is under %s\", status.RuntimeDir, avoid)\n\t}\n}\n\n")
	}
	body.WriteString("func pathWithin(path, root string) bool {\n")
	body.WriteString("\trel, err := filepath.Rel(root, path)\n")
	body.WriteString("\tif err != nil {\n\t\treturn false\n\t}\n")
	body.WriteString("\tif rel == \".\" {\n\t\treturn true\n\t}\n")
	body.WriteString("\tprefix := \"..\" + string(filepath.Separator)\n")
	body.WriteString("\treturn rel != \"..\" && (len(rel) < len(prefix) || rel[:len(prefix)] != prefix)\n}\n")
	path := filepath.Join(outputDir, "internal", "device", "cache_isolation_test.go")
	require.NoError(t, os.WriteFile(path, []byte(body.String()), 0o644))
}

func writeLegacyConfigProbe(t *testing.T, outputDir, cliName, prefix, format, defaultBase string, scrub bool) {
	t.Helper()
	scrubLiteral := "false"
	if scrub {
		scrubLiteral = "true"
	}
	var body strings.Builder
	body.WriteString("package config\n\nimport (\n\t\"bytes\"\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n)\n\n")
	body.WriteString("func TestPrintedConfigEnvSkipsLegacyHome(t *testing.T) {\n")
	body.WriteString("\tt.Run(\"missing relocated config reads legacy home\", func(t *testing.T) {\n")
	body.WriteString("\t\thome, iso := legacyHomes(t)\n")
	body.WriteString("\t\tlegacy := plantLegacyConfig(t, home)\n")
	body.WriteString("\t\tbefore := readLegacy(t, legacy)\n")
	body.WriteString("\t\tsetLegacyEnv(t, home, iso, \"\")\n")
	body.WriteString("\t\tcfg := mustLoadConfig(t)\n")
	body.WriteString("\t\tif cfg.BaseURL != \"https://legacy.invalid\" {\n\t\t\tt.Fatalf(\"base_url = %q\", cfg.BaseURL)\n\t\t}\n")
	body.WriteString("\t\tif cfg.AuthHeaderVal != \"legacy-secret\" {\n\t\t\tt.Fatalf(\"auth_header = %q\", cfg.AuthHeaderVal)\n\t\t}\n")
	body.WriteString("\t\tif err := cfg.save(); err != nil {\n\t\t\tt.Fatal(err)\n\t\t}\n")
	body.WriteString("\t\tafter := readLegacy(t, legacy)\n")
	body.WriteString("\t\tif " + scrubLiteral + " {\n")
	body.WriteString("\t\t\tif bytes.Contains(after, []byte(\"legacy-secret\")) {\n\t\t\t\tt.Fatal(\"save left the legacy credential in place\")\n\t\t\t}\n")
	body.WriteString("\t\t} else if !bytes.Equal(before, after) {\n\t\t\tt.Fatal(\"save rewrote the legacy config\")\n\t\t}\n")
	body.WriteString("\t})\n")
	body.WriteString("\tt.Run(\"explicit config file skips legacy home\", func(t *testing.T) {\n")
	body.WriteString("\t\thome, iso := legacyHomes(t)\n")
	body.WriteString("\t\tlegacy := plantLegacyConfig(t, home)\n")
	body.WriteString("\t\tbefore := readLegacy(t, legacy)\n")
	body.WriteString("\t\tsetLegacyEnv(t, home, iso, filepath.Join(iso, \"config." + format + "\"))\n")
	body.WriteString("\t\tcfg := mustLoadConfig(t)\n")
	body.WriteString("\t\tif cfg.BaseURL != " + strconv.Quote(defaultBase) + " {\n\t\t\tt.Fatalf(\"base_url = %q\", cfg.BaseURL)\n\t\t}\n")
	body.WriteString("\t\tif cfg.AuthHeaderVal != \"\" {\n\t\t\tt.Fatalf(\"auth_header = %q\", cfg.AuthHeaderVal)\n\t\t}\n")
	body.WriteString("\t\tif err := cfg.save(); err != nil {\n\t\t\tt.Fatal(err)\n\t\t}\n")
	body.WriteString("\t\tif !bytes.Equal(before, readLegacy(t, legacy)) {\n\t\t\tt.Fatal(\"save rewrote the legacy config\")\n\t\t}\n")
	body.WriteString("\t})\n}\n\n")
	body.WriteString("func legacyHomes(t *testing.T) (string, string) {\n\tt.Helper()\n\treturn t.TempDir(), t.TempDir()\n}\n\n")
	body.WriteString("func plantLegacyConfig(t *testing.T, home string) string {\n")
	body.WriteString("\tt.Helper()\n")
	body.WriteString("\tpath := filepath.Join(home, \".config\", " + strconv.Quote(cliName) + ", \"config." + format + "\")\n")
	body.WriteString("\tif err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {\n\t\tt.Fatal(err)\n\t}\n")
	body.WriteString("\tbody := " + strconv.Quote(legacyConfigBody(format)) + "\n")
	body.WriteString("\tif err := os.WriteFile(path, []byte(body), 0o600); err != nil {\n\t\tt.Fatal(err)\n\t}\n")
	body.WriteString("\treturn path\n}\n\n")
	body.WriteString("func setLegacyEnv(t *testing.T, home, iso, configFile string) {\n")
	body.WriteString("\tt.Helper()\n")
	body.WriteString("\tt.Setenv(\"MYAPI_TOKEN\", \"inherited-token\")\n")
	body.WriteString("\tt.Setenv(" + strconv.Quote(prefix+"_BASE_URL") + ", \"https://inherited.example\")\n")
	body.WriteString("\tt.Setenv(\"PRINTING_PRESS_VERIFY\", \"1\")\n")
	body.WriteString("\tt.Setenv(\"PRINTING_PRESS_DOGFOOD\", \"1\")\n")
	body.WriteString("\tt.Setenv(\"HOME\", home)\n\tt.Setenv(\"USERPROFILE\", home)\n")
	body.WriteString("\tfor _, key := range []string{\"XDG_CONFIG_HOME\", \"XDG_DATA_HOME\", \"XDG_STATE_HOME\", \"XDG_CACHE_HOME\", \"MYAPI_TOKEN\", \"PRINTING_PRESS_VERIFY\", \"PRINTING_PRESS_DOGFOOD\", " +
		strconv.Quote(prefix+"_CONFIG_DIR") + ", " + strconv.Quote(prefix+"_DATA_DIR") + ", " +
		strconv.Quote(prefix+"_STATE_DIR") + ", " + strconv.Quote(prefix+"_CACHE_DIR") + ", " +
		strconv.Quote(prefix+"_BASE_URL") + "} {\n")
	body.WriteString("\t\tt.Setenv(key, \"\")\n\t}\n")
	body.WriteString("\tt.Setenv(" + strconv.Quote(prefix+"_HOME") + ", iso)\n")
	body.WriteString("\tt.Setenv(" + strconv.Quote(prefix+"_CONFIG") + ", configFile)\n}\n\n")
	body.WriteString("func mustLoadConfig(t *testing.T) *Config {\n\tt.Helper()\n\tcfg, err := Load(\"\")\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\treturn cfg\n}\n\n")
	body.WriteString("func readLegacy(t *testing.T, path string) []byte {\n\tt.Helper()\n\tdata, err := os.ReadFile(path)\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\treturn data\n}\n")
	path := filepath.Join(outputDir, "internal", "config", "legacy_config_test.go")
	require.NoError(t, os.WriteFile(path, []byte(body.String()), 0o644))
}

func legacyConfigBody(format string) string {
	if format == "toml" {
		return "base_url = \"https://legacy.invalid\"\nauth_header = \"legacy-secret\"\n"
	}
	return "{\"base_url\":\"https://legacy.invalid\",\"auth_header\":\"legacy-secret\"}\n"
}
