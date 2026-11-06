package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChooseDataPipelineProbeResource(t *testing.T) {
	t.Parallel()

	t.Run("github-shaped cli keeps repos", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "issues", "repos", "users")

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Equal(t, "repos", resource)
		assert.Empty(t, detail)
	})

	t.Run("non-github cli uses the first safe declared resource", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "videos", "social-accounts")

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Equal(t, "videos", resource)
		assert.Empty(t, detail)
	})

	t.Run("missing repos does not invent one", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "videos")

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Equal(t, "videos", resource)
		assert.NotEqual(t, "repos", resource)
		assert.Empty(t, detail)
	})

	t.Run("empty resource list fails closed", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t)

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Empty(t, resource)
		assert.Contains(t, detail, "refusing unscoped sync")
	})

	t.Run("flag-shaped names are not probe targets", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "--full", "-rf", "videos --full", "videos,accounts", "videos")

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Equal(t, "videos", resource)
		assert.Empty(t, detail)
	})

	t.Run("only flag-shaped names fail closed", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "--full", "repos --full", "videos,social-accounts")

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Empty(t, resource)
		assert.Contains(t, detail, "refusing unscoped sync")
	})

	t.Run("manifest hint overrides repos", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "repos", "videos")
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{
			SpecFormat:         "openapi3",
			VerifySyncResource: "videos",
		}))

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Equal(t, "videos", resource)
		assert.Empty(t, detail)
	})

	t.Run("manifest exclude drops a resource before the fallback choice", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "social-accounts", "videos")
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{
			SpecFormat:        "openapi3",
			VerifySyncExclude: []string{"social-accounts"},
		}))

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Equal(t, "videos", resource)
		assert.Empty(t, detail)
	})

	t.Run("excluding every declared resource fails closed", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "videos", "accounts")
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{
			SpecFormat:        "openapi3",
			VerifySyncExclude: []string{"videos", "accounts"},
		}))

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Empty(t, resource)
		assert.Contains(t, detail, "refusing unscoped sync")
	})

	t.Run("unsafe hint does not fall through to another resource", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "videos")
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{
			SpecFormat:         "openapi3",
			VerifySyncResource: "--full",
		}))

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Empty(t, resource)
		assert.Contains(t, detail, "not a safe")
		assert.Contains(t, detail, "refusing unscoped sync")
	})

	t.Run("excluded hint does not fall through to another resource", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "videos", "repos")
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{
			SpecFormat:         "openapi3",
			VerifySyncResource: "videos",
			VerifySyncExclude:  []string{"videos"},
		}))

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Empty(t, resource)
		assert.Contains(t, detail, "not an allowed declared")
		assert.Contains(t, detail, "refusing unscoped sync")
	})

	t.Run("excluding repos uses the next safe declared resource", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "issues", "repos", "users")
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{
			SpecFormat:        "openapi3",
			VerifySyncExclude: []string{"repos"},
		}))

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Equal(t, "issues", resource)
		assert.Empty(t, detail)
	})

	t.Run("hint outside the declared list does not widen the probe", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "videos")
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{
			SpecFormat:         "openapi3",
			VerifySyncResource: "repos",
		}))

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Empty(t, resource)
		assert.Contains(t, detail, "not an allowed declared")
	})

	t.Run("research hint is used when the manifest does not name one", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "repos", "videos")
		writeTestFile(t, filepath.Join(dir, "research.json"), `{
			"api_name": "example",
			"verify": {"sync_resource": "videos", "sync_exclude": ["repos"]}
		}`)

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Equal(t, "videos", resource)
		assert.Empty(t, detail)
	})

	t.Run("manifest hint wins over research", func(t *testing.T) {
		dir := seedRunnableSyncProbeCLI(t, "repos", "videos", "clips")
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{
			SpecFormat:         "openapi3",
			VerifySyncResource: "clips",
		}))
		writeTestFile(t, filepath.Join(dir, "research.json"), `{
			"api_name": "example",
			"verify": {"sync_resource": "videos"}
		}`)

		resource, detail := chooseDataPipelineProbeResource(dir)

		assert.Equal(t, "clips", resource)
		assert.Empty(t, detail)
	})

	t.Run("no cli directory fails closed", func(t *testing.T) {
		resource, detail := chooseDataPipelineProbeResource("")

		assert.Empty(t, resource)
		assert.Contains(t, detail, "refusing unscoped sync")
	})
}

func TestParseDefaultSyncResources(t *testing.T) {
	t.Parallel()

	names, ok := parseDefaultSyncResources(`package cli

func defaultSyncResources() []string {
	return []string{
		"currencies",
		"projects",
		"tickets",
	}
}

func other() []string {
	return []string{"should-not-match"}
}
`)
	require.True(t, ok)
	assert.Equal(t, []string{"currencies", "projects", "tickets"}, names)

	names, ok = parseDefaultSyncResources("package cli\n\nfunc defaultSyncResources() []string {\n\treturn []string{}\n}\n")
	require.True(t, ok)
	assert.Empty(t, names)

	names, ok = parseDefaultSyncResources("package cli\n\nvar defaultSyncResources = []string{\"records\"}\n")
	require.True(t, ok)
	assert.Equal(t, []string{"records"}, names)

	_, ok = parseDefaultSyncResources("package cli\n\nfunc notDefault() []string { return []string{\"items\"} }\n")
	assert.False(t, ok)

	names, ok = parseDefaultSyncResources(`package cli

// func defaultSyncResources() []string { return []string{"repos", "--full"} }
func defaultSyncResources() []string {
	// previously []string{"repos", "--full", "videos,accounts"}
	/* []string{"--full", "repos"}
	func notADecl() []string { return []string{"nope"} }
	*/
	return []string{
		"videos",
	}
}
`)
	require.True(t, ok)
	assert.Equal(t, []string{"videos"}, names)
}

func TestDataPipelineSyncProbeStaysScoped(t *testing.T) {
	t.Parallel()

	t.Run("github-shaped live probe syncs repos only", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "argv.log")
		binary := buildScopedSyncProbeBinary(t)
		dir := seedRunnableSyncProbeCLI(t, "issues", "repos")
		env := append(os.Environ(),
			"PP_SYNC_PROBE_LOG="+logPath,
			"PP_SYNC_PROBE_RESOURCE=repos",
			"PP_SYNC_PROBE_REQUIRE_MAX_PAGES=1",
		)

		pass, detail := runDataPipelineTest(binary, dir, "live", func() []string { return env }, 1)

		assert.True(t, pass, detail)
		assert.Contains(t, detail, "sync --resources repos")
		assertSyncLogScopedTo(t, logPath, "repos")
	})

	t.Run("non-github probe is scoped and never falls back to unscoped full", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "argv.log")
		binary := buildScopedSyncProbeBinary(t)
		dir := seedRunnableSyncProbeCLI(t, "videos", "social-accounts")
		env := append(os.Environ(),
			"PP_SYNC_PROBE_LOG="+logPath,
			"PP_SYNC_PROBE_RESOURCE=videos",
			"PP_SYNC_PROBE_REQUIRE_MAX_PAGES=1",
		)

		pass, detail := runDataPipelineTest(binary, dir, "live", func() []string { return env }, 1)

		assert.True(t, pass, detail)
		assert.Contains(t, detail, "sync --resources videos")
		log := assertSyncLogScopedTo(t, logPath, "videos")
		assert.NotContains(t, log, "social-accounts")
		assert.NotContains(t, log, "UNSCOPED")
		assert.Contains(t, log, "--max-pages 1")
	})

	t.Run("scoped failure does not widen to an unscoped sync", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "argv.log")
		binary := buildScopedSyncProbeBinary(t)
		dir := seedRunnableSyncProbeCLI(t, "videos", "social-accounts")
		env := append(os.Environ(), "PP_SYNC_PROBE_LOG="+logPath, "PP_SYNC_PROBE_FAIL=1")

		pass, detail := runDataPipelineTest(binary, dir, "mock", func() []string { return env }, 1)

		assert.False(t, pass)
		assert.Equal(t, "FAIL: sync crashed", detail)
		log := assertSyncLogScopedTo(t, logPath, "videos")
		assert.NotContains(t, log, "social-accounts")
		syncLines := 0
		for line := range strings.SplitSeq(strings.TrimSpace(log), "\n") {
			if strings.HasPrefix(line, "sync ") {
				syncLines++
				assert.Contains(t, line, "--resources videos")
			}
		}
		assert.Equal(t, 1, syncLines)
	})

	t.Run("unknown full flag retries the same resource without full", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "argv.log")
		binary := buildScopedSyncProbeBinary(t)
		dir := seedRunnableSyncProbeCLI(t, "videos")
		env := append(os.Environ(), "PP_SYNC_PROBE_LOG="+logPath, "PP_SYNC_PROBE_REJECT_FULL=1")

		pass, detail := runDataPipelineTest(binary, dir, "mock", func() []string { return env }, 1)

		assert.True(t, pass, detail)
		log := assertSyncLogScopedTo(t, logPath, "videos")
		var syncLines []string
		for line := range strings.SplitSeq(strings.TrimSpace(log), "\n") {
			if strings.HasPrefix(line, "sync ") {
				syncLines = append(syncLines, line)
			}
		}
		require.Len(t, syncLines, 2)
		assert.Contains(t, syncLines[0], "--full")
		assert.NotContains(t, syncLines[1], "--full")
		assert.Contains(t, syncLines[1], "--resources videos")
	})

	t.Run("no safe resource does not run sync", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "argv.log")
		binary := buildScopedSyncProbeBinary(t)
		dir := seedRunnableSyncProbeCLI(t, "--full", "videos,accounts")
		env := append(os.Environ(), "PP_SYNC_PROBE_LOG="+logPath)

		pass, detail := runDataPipelineTest(binary, dir, "live", func() []string { return env }, 1)

		assert.False(t, pass)
		assert.Contains(t, detail, "refusing unscoped sync")
		_, err := os.Stat(logPath)
		assert.True(t, os.IsNotExist(err), "sync was invoked: log exists")
	})
}

func TestDataPipelineSyncTimeoutCoversOneLivePage(t *testing.T) {
	t.Parallel()

	assert.Equal(t, mockDataPipelineSyncTimeout, dataPipelineSyncTimeout("mock"))
	assert.GreaterOrEqual(t, dataPipelineSyncTimeout("live"), 90*time.Second)
	assert.Equal(t, liveDataPipelineSyncTimeout, dataPipelineSyncTimeout("live"))
}

func assertSyncLogScopedTo(t *testing.T, logPath, resource string) string {
	t.Helper()

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	log := string(data)
	assert.Contains(t, log, "--resources "+resource)
	assert.NotContains(t, log, "UNSCOPED")
	for line := range strings.SplitSeq(strings.TrimSpace(log), "\n") {
		if strings.HasPrefix(line, "sync ") {
			assert.Contains(t, line, "--resources "+resource)
		}
	}
	return log
}

func buildScopedSyncProbeBinary(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(1)
	}
	if logPath := os.Getenv("PP_SYNC_PROBE_LOG"); logPath != "" {
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			os.Exit(1)
		}
		fmt.Fprintln(f, strings.Join(args, " "))
		f.Close()
	}
	switch args[0] {
	case "sync":
		resource := flagValue(args[1:], "--resources")
		if resource == "" || strings.Contains(resource, ",") || strings.HasPrefix(resource, "-") {
			fmt.Fprintln(os.Stderr, "UNSCOPED")
			os.Exit(1)
		}
		for _, arg := range args[1:] {
			if arg == "--full" && os.Getenv("PP_SYNC_PROBE_REJECT_FULL") == "1" {
				fmt.Fprintln(os.Stderr, "unknown flag: --full")
				os.Exit(1)
			}
		}
		if os.Getenv("PP_SYNC_PROBE_FAIL") == "1" {
			fmt.Fprintln(os.Stderr, "sync failed")
			os.Exit(1)
		}
		if want := os.Getenv("PP_SYNC_PROBE_RESOURCE"); want != "" && resource != want {
			fmt.Fprintf(os.Stderr, "scoped to %s, want %s\n", resource, want)
			os.Exit(1)
		}
		if os.Getenv("PP_SYNC_PROBE_REQUIRE_MAX_PAGES") == "1" && !hasFlagValue(args[1:], "--max-pages", "1") {
			fmt.Fprintln(os.Stderr, "missing --max-pages 1")
			os.Exit(1)
		}
		dbPath := flagValue(args[1:], "--db")
		if dbPath == "" {
			os.Exit(1)
		}
		if err := os.WriteFile(dbPath+".marker", []byte(dbPath), 0o644); err != nil {
			os.Exit(1)
		}
		return
	case "sql":
		dbPath := flagValue(args[1:], "--db")
		if dbPath == "" {
			os.Exit(1)
		}
		usedDB, err := os.ReadFile(dbPath + ".marker")
		if err != nil || string(usedDB) != dbPath {
			os.Exit(1)
		}
		query := args[len(args)-1]
		if strings.Contains(query, "sqlite_master") {
			fmt.Println("items")
			return
		}
		if strings.Contains(query, "count(*)") {
			fmt.Println(1)
			return
		}
	}
	os.Exit(1)
}

func flagValue(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func hasFlagValue(args []string, flag, want string) bool {
	return flagValue(args, flag) == want
}
`)
	binaryPath := filepath.Join(dir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))
	return binaryPath
}
