package mcpsync

import (
	"encoding/json"
	"go/ast"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	demoSeamLine   = "\tfmt.Fprintln(os.Stderr, \"demo seam\") // PATCH(demo-seam, 2026-10-26): hand-owned call site"
	demoSeamNeedle = "fmt.Fprintln(os.Stderr, \"demo seam\") // PATCH(demo-seam"
)

func TestPreserveRecordedPatchSeamsReinsertsOnlyTheRecordedLine(t *testing.T) {
	t.Parallel()

	before := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n" +
		demoSeamLine + "\n\t// unrecorded aside\n\tfmt.Println(\"old\")\n}\n"
	after := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n\tfmt.Println(\"generated\")\n}\n"
	got := preserveSeam(t, "cmd/seamcli-pp-mcp/main.go", before, after, demoRecord("cmd/seamcli-pp-mcp/main.go"))

	assert.Contains(t, got, demoSeamNeedle)
	assert.Contains(t, got, "fmt.Println(\"generated\")")
	assert.NotContains(t, got, "unrecorded aside")
	assert.NotContains(t, got, "fmt.Println(\"old\")")
	assert.Less(t, strings.Index(got, "mcptools.RegisterTools(s)"), strings.Index(got, demoSeamNeedle))
	assert.Equal(t, 1, strings.Count(got, "\"fmt\""))
}

func TestPreserveRecordedPatchSeamsKeepsConsecutiveLinesInOrder(t *testing.T) {
	t.Parallel()

	second := "\tmcptools.RegisterKeptUnderReadOnly(s) // PATCH(keep-local"
	before := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n" + demoSeamLine + "\n" + second + "\n\tfmt.Println(\"old\")\n}\n"
	after := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n\tfmt.Println(\"generated\")\n}\n"
	rec := demoRecord("cmd/app-pp-mcp/main.go")
	rec.Markers = append(rec.Markers, "PATCH(keep-local")
	got := preserveSeam(t, "cmd/app-pp-mcp/main.go", before, after, rec)

	assert.Less(t, strings.Index(got, demoSeamNeedle), strings.Index(got, "PATCH(keep-local"))
	assert.Contains(t, got, "fmt.Println(\"generated\")")
	assert.NotContains(t, got, "fmt.Println(\"old\")")
}

func TestPreserveRecordedPatchSeamsInsertsBeforeFollowingAnchor(t *testing.T) {
	t.Parallel()

	before := "package oldpkg\n\nfunc removed() {\n" + demoSeamLine + "\n\tfmt.Println(\"keep-me-unique\")\n}\n"
	after := "package main\n\nfunc main() {\n\tfmt.Println(\"keep-me-unique\")\n}\n"
	got := preserveSeam(t, "cmd/app-pp-mcp/main.go", before, after, demoRecord("cmd/app-pp-mcp/main.go"))

	assert.Less(t, strings.Index(got, demoSeamNeedle), strings.Index(got, "keep-me-unique"))
	assert.Contains(t, got, "func main()")
}

func TestPreserveRecordedPatchSeamsLeavesFileWhenNeedleAlreadyPresent(t *testing.T) {
	t.Parallel()

	before := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n" + demoSeamLine + "\n\tfmt.Println(\"old\")\n}\n"
	after := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n" + demoSeamLine + "\n\tfmt.Println(\"generated\")\n}\n"
	dir, path := writeRelFile(t, "cmd/app-pp-mcp/main.go", after)
	err := preserveRecordedPatchSeams(dir, []pipeline.PatchRecord{demoRecord("cmd/app-pp-mcp/main.go")}, map[string][]byte{
		"cmd/app-pp-mcp/main.go": []byte(before),
	})
	require.NoError(t, err)
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, after, string(got))
}

func TestPreserveRecordedPatchSeamsNoRecordsDoesNotWrite(t *testing.T) {
	t.Parallel()

	dir, path := writeRelFile(t, "cmd/app-pp-mcp/main.go", "package main\n")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, preserveRecordedPatchSeams(dir, nil, map[string][]byte{
		"cmd/app-pp-mcp/main.go": []byte("package main\n" + demoSeamLine + "\n"),
	}))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestPreserveRecordedPatchSeamsNamesUnanchoredLine(t *testing.T) {
	t.Parallel()

	before := "package oldpkg\n\nfunc removed() {\n" + demoSeamLine + "\n\tfmt.Println(\"also-gone\")\n}\n"
	after := "package main\n\nfunc main() {\n\tfmt.Println(\"one\")\n}\n\nfunc other() {\n\tfmt.Println(\"two\")\n}\n"
	dir, path := writeRelFile(t, "cmd/app-pp-mcp/main.go", after)
	err := preserveRecordedPatchSeams(dir, []pipeline.PatchRecord{demoRecord("cmd/app-pp-mcp/main.go")}, map[string][]byte{
		"cmd/app-pp-mcp/main.go": []byte(before),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "demo-seam"`)
	assert.Contains(t, err.Error(), "demo seam")
	assert.Contains(t, err.Error(), "cmd/app-pp-mcp/main.go")
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, after, string(got))
}

func TestPreserveRecordedPatchSeamsDoesNotWriteUnparseableGo(t *testing.T) {
	t.Parallel()

	before := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n\tNOT_VALID_GO( // PATCH(demo-seam\n}\n"
	after := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n\tfmt.Println(\"generated\")\n}\n"
	rec := demoRecord("cmd/app-pp-mcp/main.go")
	rec.Markers = []string{"PATCH(demo-seam"}
	dir, path := writeRelFile(t, "cmd/app-pp-mcp/main.go", after)
	err := preserveRecordedPatchSeams(dir, []pipeline.PatchRecord{rec}, map[string][]byte{
		"cmd/app-pp-mcp/main.go": []byte(before),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "demo-seam"`)
	assert.Contains(t, err.Error(), "NOT_VALID_GO")
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, after, string(got))
}

func TestPreserveRecordedPatchSeamsSkipsToolsGo(t *testing.T) {
	t.Parallel()

	after := "package mcp\n\nfunc RegisterTools() {}\n"
	dir, path := writeRelFile(t, regeneratedMCPToolsFile, after)
	rec := demoRecord(regeneratedMCPToolsFile)
	err := preserveRecordedPatchSeams(dir, []pipeline.PatchRecord{rec}, map[string][]byte{
		regeneratedMCPToolsFile: []byte(after + demoSeamLine + "\n"),
	})
	require.NoError(t, err)
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, after, string(got))
	writePatchFile(t, dir, "demo-seam.json", rec)
	valErr := pipeline.ValidatePatchRecords(dir)
	require.Error(t, valErr)
	assert.Contains(t, valErr.Error(), `patch "demo-seam"`)
	assert.Contains(t, valErr.Error(), "demo seam")
}

func TestPreserveRecordedPatchSeamsMissingFileNamesThePatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := preserveRecordedPatchSeams(dir, []pipeline.PatchRecord{demoRecord("cmd/app-pp-mcp/main.go")}, map[string][]byte{
		"cmd/app-pp-mcp/main.go": []byte("package main\n\nfunc main() {\n" + demoSeamLine + "\n}\n"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "demo-seam"`)
	assert.Contains(t, err.Error(), "missing after sync")
	assert.Contains(t, err.Error(), "cmd/app-pp-mcp/main.go")
}

func TestPreserveRecordedPatchSeamsAddsAliasImport(t *testing.T) {
	t.Parallel()

	before := "package main\n\nimport (\n\t\"fmt\"\n\tmetrics \"example.com/metrics\"\n)\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n\tmetrics.Count() // PATCH(demo-seam\n}\n"
	after := "package main\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n\tfmt.Println(\"generated\")\n}\n"
	rec := demoRecord("cmd/app-pp-mcp/main.go")
	rec.Markers = []string{"metrics.Count() // PATCH(demo-seam"}
	got := preserveSeam(t, "cmd/app-pp-mcp/main.go", before, after, rec)
	assert.Contains(t, got, "metrics \"example.com/metrics\"")
	assert.Contains(t, got, "metrics.Count() // PATCH(demo-seam")
	assert.Contains(t, got, "fmt.Println(\"generated\")")
	assert.Equal(t, 1, strings.Count(got, "example.com/metrics"))
}

func TestPreserveRecordedPatchSeamsIgnoresIdentPrefixOfImportName(t *testing.T) {
	t.Parallel()

	before := "package main\n\nimport (\n\tfmt \"example.com/fmt\"\n)\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n\tmyfmt.Count() // PATCH(demo-seam\n}\n"
	after := "package main\n\nimport (\n\t\"os\"\n)\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n\tfmt.Println(\"generated\")\n}\n"
	rec := demoRecord("cmd/app-pp-mcp/main.go")
	rec.Markers = []string{"myfmt.Count() // PATCH(demo-seam"}
	got := preserveSeam(t, "cmd/app-pp-mcp/main.go", before, after, rec)
	assert.NotContains(t, got, "example.com/fmt")
	assert.Contains(t, got, "myfmt.Count() // PATCH(demo-seam")
}

func TestPreserveRecordedPatchSeamsInsertsNonGoFileWithoutParsing(t *testing.T) {
	t.Parallel()

	before := "intro\n" + demoSeamLine + "\n"
	after := "intro\nrest\n"
	got := preserveSeam(t, "notes/seam.txt", before, after, demoRecord("notes/seam.txt"))
	assert.Contains(t, got, demoSeamNeedle)
	assert.Contains(t, got, "rest")
}

func TestPreserveRecordedPatchSeamsSkipsMultilineNeedle(t *testing.T) {
	t.Parallel()

	before := "package main\n\nfunc main() {\n\tfmt.Println(\"demo\")\n\tfmt.Println(\"seam\")\n}\n"
	after := "package main\n\nfunc main() {\n\tfmt.Println(\"generated\")\n}\n"
	rec := demoRecord("cmd/app-pp-mcp/main.go")
	rec.Markers = []string{"fmt.Println(\"demo\")\n\tfmt.Println(\"seam\")"}
	dir, path := writeRelFile(t, "cmd/app-pp-mcp/main.go", after)
	writePatchFile(t, dir, "demo-seam.json", rec)
	err := preserveRecordedPatchSeams(dir, []pipeline.PatchRecord{rec}, map[string][]byte{
		"cmd/app-pp-mcp/main.go": []byte(before),
	})
	require.NoError(t, err)
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, after, string(got))
	valErr := pipeline.ValidatePatchRecords(dir)
	require.Error(t, valErr)
	assert.Contains(t, valErr.Error(), `patch "demo-seam"`)
}

func TestPreserveRecordedPatchSeamsUsesCallSiteNeedle(t *testing.T) {
	t.Parallel()

	line := "\tdefer cli.CleanupNoLocalStore() // kept"
	before := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n" + line + "\n}\n"
	after := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n}\n"
	rec := demoRecord("cmd/app-pp-mcp/main.go")
	rec.Markers = nil
	rec.CallSites = []string{"cli.CleanupNoLocalStore()"}
	got := preserveSeam(t, "cmd/app-pp-mcp/main.go", before, after, rec)
	assert.Contains(t, got, "cli.CleanupNoLocalStore()")
}

func TestPreserveRecordedPatchSeamsTwoLinesSharingANeedle(t *testing.T) {
	t.Parallel()

	first := "\tfmt.Fprintln(os.Stderr, \"one\") // PATCH(demo-seam"
	second := "\tfmt.Fprintln(os.Stderr, \"two\") // PATCH(demo-seam"
	before := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n" + first + "\n" + second + "\n}\n"
	after := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n}\n"
	rec := demoRecord("cmd/app-pp-mcp/main.go")
	rec.Markers = []string{"PATCH(demo-seam"}
	got := preserveSeam(t, "cmd/app-pp-mcp/main.go", before, after, rec)
	assert.Contains(t, got, "\"one\")")
	assert.Contains(t, got, "\"two\")")
	assert.Less(t, strings.Index(got, "\"one\")"), strings.Index(got, "\"two\")"))
}

func TestPreserveRecordedPatchSeamsEmptyIDUsesSource(t *testing.T) {
	t.Parallel()

	before := "package oldpkg\n\nfunc removed() {\n" + demoSeamLine + "\n}\n"
	after := "package main\n\nfunc main() {\n\tfmt.Println(\"one\")\n}\n\nfunc other() {\n\tfmt.Println(\"two\")\n}\n"
	dir, _ := writeRelFile(t, "cmd/app-pp-mcp/main.go", after)
	rec := demoRecord("cmd/app-pp-mcp/main.go")
	rec.ID = ""
	rec.Source = ".printing-press-patches/demo-seam.json"
	err := preserveRecordedPatchSeams(dir, []pipeline.PatchRecord{rec}, map[string][]byte{
		"cmd/app-pp-mcp/main.go": []byte(before),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), rec.Source)
}

func TestPreserveRecordedPatchSeamsStopsAtAnchorWindow(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("package oldpkg\n\nfunc removed() {\n\tmcptools.RegisterTools(s)\n")
	for range recordedSeamAnchorWindow {
		b.WriteString("\tfmt.Println(\"gone\")\n")
	}
	b.WriteString(demoSeamLine + "\n}\n")
	after := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n\tfmt.Println(\"one\")\n}\n\nfunc other() {\n\tfmt.Println(\"two\")\n}\n"
	dir, path := writeRelFile(t, "cmd/app-pp-mcp/main.go", after)
	err := preserveRecordedPatchSeams(dir, []pipeline.PatchRecord{demoRecord("cmd/app-pp-mcp/main.go")}, map[string][]byte{
		"cmd/app-pp-mcp/main.go": []byte(b.String()),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "demo-seam"`)
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, after, string(got))
	assert.NotContains(t, string(got), demoSeamNeedle)
}

func TestSpliceRecordedLineRejectsIndexPastEnd(t *testing.T) {
	t.Parallel()

	_, err := spliceRecordedLine([]byte("package main\n"), []byte("package main\n"), seamLine{
		patchID: "demo-seam",
		rel:     "cmd/app-pp-mcp/main.go",
		line:    demoSeamLine,
		index:   9,
		needle:  demoSeamNeedle,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "demo-seam"`)
}

func TestSnapshotRecordedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "cmd"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main\n"), 0o644))
	outside := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o644))

	got, err := snapshotRecordedFiles(dir, []pipeline.PatchRecord{{
		ID:    "demo-seam",
		Files: []string{"./cmd/main.go", "../secret", "/etc/passwd", "", ".", "cmd/../../etc/passwd", "missing.go"},
	}})
	require.NoError(t, err)
	assert.Equal(t, map[string][]byte{"cmd/main.go": []byte("package main\n")}, got)

	_, err = snapshotRecordedFiles(dir, []pipeline.PatchRecord{{
		ID:    "demo-seam",
		Files: []string{"cmd"},
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cmd")
}

func TestSafePatchRel(t *testing.T) {
	t.Parallel()

	rel, ok := safePatchRel("./cmd/app/main.go")
	assert.True(t, ok)
	assert.Equal(t, "cmd/app/main.go", rel)

	for _, raw := range []string{"", ".", "/etc/passwd", "../secret", "cmd/../../etc/passwd"} {
		_, ok := safePatchRel(raw)
		assert.False(t, ok, raw)
	}
}

func TestReferencesImport(t *testing.T) {
	t.Parallel()

	assert.False(t, referencesImport("fmt.Println", ""))
	assert.False(t, referencesImport("fmt.Println", "_"))
	assert.False(t, referencesImport("fmt.Println", "."))
	assert.False(t, referencesImport("myfmt.Count()", "fmt"))
	assert.True(t, referencesImport("fmt.Println", "fmt"))
	assert.True(t, referencesImport("\tmetrics.Count()", "metrics"))
}

func TestImportLocalName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "metrics", importLocalName(&ast.ImportSpec{Name: ast.NewIdent("metrics")}, "example.com/metrics"))
	assert.Equal(t, "metrics", importLocalName(&ast.ImportSpec{}, "example.com/metrics"))
	assert.Equal(t, "fmt", importLocalName(&ast.ImportSpec{}, "fmt"))
}

func TestAddImportLine(t *testing.T) {
	t.Parallel()

	src := "package main\n\nfunc main() {}\n"
	assert.Equal(t, src, addImportLine(src, "not an import"))
	assert.Equal(t, src, addImportLine(src, `"fmt`))
	assert.Equal(t, src, addImportLine(src, ""))
	withImport := addImportLine(src, `"fmt"`)
	assert.Contains(t, withImport, `"fmt"`)
	assert.Equal(t, withImport, addImportLine(withImport, `"fmt"`))

	blocked := "package main\n\nimport (\n\t\"os\"\n)\n"
	added := addImportLine(blocked, "metrics \"example.com/metrics\"")
	assert.Contains(t, added, "\tmetrics \"example.com/metrics\"\n")
	assert.Contains(t, added, "\"os\"")
}

func TestMissingImportLinesParseFailure(t *testing.T) {
	t.Parallel()

	assert.Nil(t, missingImportLines("not go", "package main\n", "fmt.Println"))
	assert.Nil(t, missingImportLines("package main\nimport \"fmt\"\n", "package", "fmt.Println"))
}

func TestPatchNeedlesDedupes(t *testing.T) {
	t.Parallel()

	got := patchNeedles(pipeline.PatchRecord{
		Marker:    " same ",
		Markers:   []string{"same", " ", "other"},
		CallSites: []string{"other", "third"},
	})
	assert.Equal(t, []string{"same", "other", "third"}, got)
}

func TestPreserveRecordedPatchSeamsLegacyIndex(t *testing.T) {
	t.Parallel()

	before := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n" + demoSeamLine + "\n}\n"
	after := "package main\n\nfunc main() {\n\tmcptools.RegisterTools(s)\n}\n"
	dir, _ := writeRelFile(t, "cmd/app-pp-mcp/main.go", after)
	index, err := json.Marshal(map[string]any{
		"patches": []pipeline.PatchRecord{demoRecord("cmd/app-pp-mcp/main.go")},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, pipeline.PatchesIndexFilename), index, 0o644))
	records, err := pipeline.LoadPatchRecords(dir)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.NoError(t, preserveRecordedPatchSeams(dir, records, map[string][]byte{
		"cmd/app-pp-mcp/main.go": []byte(before),
	}))
	require.NoError(t, pipeline.ValidatePatchRecords(dir))
}

func TestUniqueWholeBlockRejectsSubstring(t *testing.T) {
	t.Parallel()

	_, _, ok := uniqueWholeBlock("mcptool\n", []string{"tool"})
	assert.False(t, ok)
	pos, ok := uniqueBlockEnd("\tmcptools.RegisterTools(s)\n\tnext\n", []string{"\tmcptools.RegisterTools(s)"})
	assert.True(t, ok)
	assert.Equal(t, len("\tmcptools.RegisterTools(s)\n"), pos)
}

func TestSyncPreservesRecordedMCPEntrypointSeam(t *testing.T) {
	t.Parallel()

	cliDir := generateSeamCLI(t)
	state, err := pipeline.InspectMCPSurface(cliDir)
	require.NoError(t, err)
	require.Equal(t, pipeline.MCPSurfaceRuntime, state.State)

	mainRel := "cmd/seamcli-pp-mcp/main.go"
	mainPath := filepath.Join(cliDir, filepath.FromSlash(mainRel))
	mainData, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	const anchor = "\tmcptools.RegisterTools(s)\n"
	require.Contains(t, string(mainData), anchor)
	planted := strings.Replace(string(mainData), anchor, anchor+demoSeamLine+"\n\t// unrecorded aside\n", 1)
	require.NotEqual(t, string(mainData), planted)
	require.NoError(t, os.WriteFile(mainPath, []byte(planted), 0o644))
	writePatchFile(t, cliDir, "demo-seam.json", demoRecord(mainRel))

	_, err = Sync(cliDir, Options{})
	require.NoError(t, err)

	got, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	assert.Contains(t, string(got), demoSeamNeedle)
	assert.Equal(t, 1, strings.Count(string(got), "demo seam"))
	assert.NotContains(t, string(got), "unrecorded aside")
	assert.Contains(t, string(got), "mcptools.RegisterTools(s)")
	require.NoError(t, pipeline.ValidatePatchRecords(cliDir))

	_, err = Sync(cliDir, Options{})
	require.NoError(t, err)
	second, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(second), "demo seam"))
	require.NoError(t, pipeline.ValidatePatchRecords(cliDir))

	vet := exec.Command("go", "vet", "-mod=mod", "./cmd/seamcli-pp-mcp")
	vet.Dir = cliDir
	output, err := vet.CombinedOutput()
	require.NoError(t, err, "preserved entrypoint must vet: %s", output)
}

func TestSyncWithoutPatchRecordsLeavesEntrypointByteIdentical(t *testing.T) {
	t.Parallel()

	withoutDir := generateSeamCLI(t)
	patches := filepath.Join(withoutDir, pipeline.PatchesDirName)
	require.NoError(t, os.RemoveAll(patches))
	_, err := os.Stat(patches)
	require.True(t, os.IsNotExist(err))

	withDir := generateSeamCLI(t)
	require.NoError(t, os.MkdirAll(filepath.Join(withDir, pipeline.PatchesDirName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(withDir, pipeline.PatchesDirName, pipeline.PatchesGitKeepName), nil, 0o644))

	mainRel := filepath.Join("cmd", "seamcli-pp-mcp", "main.go")
	toolsRel := filepath.Join("internal", "mcp", "tools.go")
	beforeWithoutMain := mustRead(t, filepath.Join(withoutDir, mainRel))
	beforeWithoutTools := mustRead(t, filepath.Join(withoutDir, toolsRel))
	beforeWithMain := mustRead(t, filepath.Join(withDir, mainRel))
	beforeWithTools := mustRead(t, filepath.Join(withDir, toolsRel))
	assert.Equal(t, beforeWithoutMain, beforeWithMain)
	assert.Equal(t, beforeWithoutTools, beforeWithTools)

	_, err = Sync(withoutDir, Options{})
	require.NoError(t, err)
	_, err = Sync(withDir, Options{})
	require.NoError(t, err)

	assert.Equal(t, beforeWithoutMain, mustRead(t, filepath.Join(withoutDir, mainRel)))
	assert.Equal(t, beforeWithMain, mustRead(t, filepath.Join(withDir, mainRel)))
	assert.Equal(t, mustRead(t, filepath.Join(withoutDir, mainRel)), mustRead(t, filepath.Join(withDir, mainRel)))
	assert.Equal(t, mustRead(t, filepath.Join(withoutDir, toolsRel)), mustRead(t, filepath.Join(withDir, toolsRel)))
	require.NoError(t, pipeline.ValidatePatchRecords(withoutDir))
	require.NoError(t, pipeline.ValidatePatchRecords(withDir))
}

func TestSyncDropsUnrecordedPatchMarker(t *testing.T) {
	t.Parallel()

	cliDir := generateSeamCLI(t)
	mainPath := filepath.Join(cliDir, "cmd", "seamcli-pp-mcp", "main.go")
	mainData, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	const anchor = "\tmcptools.RegisterTools(s)\n"
	planted := strings.Replace(string(mainData), anchor, anchor+"\t// PATCH(unrecorded-seam): not in a patch record\n", 1)
	require.NoError(t, os.WriteFile(mainPath, []byte(planted), 0o644))

	_, err = Sync(cliDir, Options{})
	require.NoError(t, err)
	got, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "unrecorded-seam")
}

func TestSyncRecordedToolsGoSeamFailsWithoutSplicing(t *testing.T) {
	t.Parallel()

	clean := generateSeamCLI(t)
	planted := generateSeamCLI(t)
	toolsRel := filepath.Join("internal", "mcp", "tools.go")
	toolsPath := filepath.Join(planted, toolsRel)
	tools, err := os.ReadFile(toolsPath)
	require.NoError(t, err)
	const marker = "// PATCH(tools-seam): do not splice this file"
	require.NoError(t, os.WriteFile(toolsPath, append(tools, []byte("\n"+marker+"\n")...), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(planted, pipeline.PatchesDirName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(planted, pipeline.PatchesDirName, "tools-seam.json"), []byte(`{
		"schema_version": 2,
		"id": "tools-seam",
		"files": ["internal/mcp/tools.go"],
		"markers": ["PATCH(tools-seam)"]
	}`), 0o644))

	_, cleanErr := Sync(clean, Options{})
	require.NoError(t, cleanErr)
	_, err = Sync(planted, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "tools-seam"`)
	assert.Contains(t, err.Error(), "PATCH(tools-seam)")
	got, readErr := os.ReadFile(toolsPath)
	require.NoError(t, readErr)
	assert.NotContains(t, string(got), "PATCH(tools-seam)")
	assert.Equal(t, mustRead(t, filepath.Join(clean, toolsRel)), got)
}

func TestSyncMalformedPatchRecordFails(t *testing.T) {
	t.Parallel()

	cliDir := generateSeamCLI(t)
	mainPath := filepath.Join(cliDir, "cmd", "seamcli-pp-mcp", "main.go")
	before := mustRead(t, mainPath)
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, pipeline.PatchesDirName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, pipeline.PatchesDirName, "broken.json"), []byte("{"), 0o644))

	_, err := Sync(cliDir, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken.json")
	assert.Contains(t, err.Error(), "reading patch records")
	assert.Equal(t, before, mustRead(t, mainPath))
}

func TestSyncUnanchoredRecordedSeamFailsNamed(t *testing.T) {
	t.Parallel()

	cliDir := generateSeamCLI(t)
	mainRel := "cmd/seamcli-pp-mcp/main.go"
	mainPath := filepath.Join(cliDir, filepath.FromSlash(mainRel))
	require.NoError(t, os.WriteFile(mainPath, []byte("package oldpkg\n\nfunc removed() {\n"+demoSeamLine+"\n}\n"), 0o644))
	writePatchFile(t, cliDir, "demo-seam.json", demoRecord(mainRel))

	_, err := Sync(cliDir, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "preserving recorded patch seams")
	assert.Contains(t, err.Error(), `patch "demo-seam"`)
	assert.Contains(t, err.Error(), "demo seam")
	got := string(mustRead(t, mainPath))
	assert.NotContains(t, got, "demo seam")
	assert.Contains(t, got, "mcptools.RegisterTools(s)")
}

func TestSyncRecordedDirectoryFailsBeforeRewrite(t *testing.T) {
	t.Parallel()

	cliDir := generateSeamCLI(t)
	mainPath := filepath.Join(cliDir, "cmd", "seamcli-pp-mcp", "main.go")
	before := mustRead(t, mainPath)
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, pipeline.PatchesDirName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, pipeline.PatchesDirName, "bad-file.json"), []byte(`{
		"schema_version": 2,
		"id": "bad-file",
		"files": ["cmd"],
		"markers": ["no such seam"]
	}`), 0o644))

	_, err := Sync(cliDir, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshotting recorded patch files")
	assert.Contains(t, err.Error(), "cmd")
	assert.Equal(t, before, mustRead(t, mainPath))
}

func TestSyncMissingRecordedNeedleFailsValidation(t *testing.T) {
	t.Parallel()

	cliDir := generateSeamCLI(t)
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, pipeline.PatchesDirName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, pipeline.PatchesDirName, "gone.json"), []byte(`{
		"schema_version": 2,
		"id": "gone-seam",
		"files": ["cmd/seamcli-pp-mcp/main.go"],
		"markers": ["this line was never planted"]
	}`), 0o644))

	_, err := Sync(cliDir, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recorded patches no longer match")
	assert.Contains(t, err.Error(), `patch "gone-seam"`)
	assert.Contains(t, err.Error(), "this line was never planted")
}

func TestReinsertRecordedLineSurfacesReadErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "cmd"), 0o755))
	err := reinsertRecordedLine(dir, []byte("package main\n"+demoSeamLine+"\n"), seamLine{
		patchID: "demo-seam",
		rel:     "cmd",
		line:    demoSeamLine,
		index:   1,
		needle:  demoSeamNeedle,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "demo-seam"`)
	assert.Contains(t, err.Error(), "cmd")
	assert.NotContains(t, err.Error(), "missing after sync")
}

func TestSyncHandEditedStillPrecedesPatchRecords(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "mcp"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "mcp", "tools.go"), []byte("package mcp\n\nfunc RegisterTools() {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, pipeline.PatchesDirName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, pipeline.PatchesDirName, "broken.json"), []byte("{"), 0o644))

	_, err := Sync(dir, Options{})
	require.ErrorIs(t, err, ErrHandEdited)
}

func generateSeamCLI(t *testing.T) string {
	t.Helper()
	apiSpec := &spec.APISpec{
		Name:    "seamcli",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/seamcli-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"widgets": {
				Description: "Widgets",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/widgets", Description: "List widgets"},
				},
			},
		},
	}
	cliDir := filepath.Join(t.TempDir(), "seamcli")
	gen := generator.New(apiSpec, cliDir)
	gen.VisionSet = generator.VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())
	specData, err := yaml.Marshal(apiSpec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "spec.yaml"), specData, 0o644))
	return cliDir
}

func demoRecord(rel string) pipeline.PatchRecord {
	return pipeline.PatchRecord{
		SchemaVersion: 2,
		ID:            "demo-seam",
		Files:         []string{rel},
		Markers:       []string{demoSeamNeedle},
	}
}

func preserveSeam(t *testing.T, rel, before, after string, rec pipeline.PatchRecord) string {
	t.Helper()
	dir, path := writeRelFile(t, rel, after)
	err := preserveRecordedPatchSeams(dir, []pipeline.PatchRecord{rec}, map[string][]byte{rel: []byte(before)})
	require.NoError(t, err)
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	return string(got)
}

func writeRelFile(t *testing.T, rel, contents string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return dir, path
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func writePatchFile(t *testing.T, dir, name string, rec pipeline.PatchRecord) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, pipeline.PatchesDirName), 0o755))
	data, err := json.MarshalIndent(rec, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, pipeline.PatchesDirName, name), append(data, '\n'), 0o644))
}
