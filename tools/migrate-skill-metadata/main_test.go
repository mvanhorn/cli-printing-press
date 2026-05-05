package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Unit tests for derivation helpers ---

func TestDeriveModule(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"latest tag", "go install github.com/x/y/cmd/z@latest", "github.com/x/y/cmd/z", false},
		{"explicit version", "go install github.com/x/y/cmd/z@v1.2.3", "github.com/x/y/cmd/z", false},
		{"no version tag", "go install github.com/x/y/cmd/z", "github.com/x/y/cmd/z", false},
		{"missing prefix", "github.com/x/y/cmd/z@latest", "", true},
		{"empty after strip", "go install @latest", "", true},
		{"path with @ in it", "go install github.com/x/y@email.com/cmd/z@latest", "github.com/x/y@email.com/cmd/z", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := deriveModule(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEmitMetadataBlock(t *testing.T) {
	want := "metadata:\n" +
		"  openclaw:\n" +
		"    requires:\n" +
		"      bins:\n" +
		"        - foo-pp-cli\n" +
		"    install:\n" +
		"      - kind: go\n" +
		"        bins: [foo-pp-cli]\n" +
		"        module: github.com/example/foo/cmd/foo-pp-cli\n"
	got := emitMetadataBlock("foo-pp-cli", "github.com/example/foo/cmd/foo-pp-cli", nil, "")
	if got != want {
		t.Errorf("emitMetadataBlock no-auth mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestEmitMetadataBlockWithAuth(t *testing.T) {
	want := "metadata:\n" +
		"  openclaw:\n" +
		"    requires:\n" +
		"      bins:\n" +
		"        - foo-pp-cli\n" +
		"      env:\n" +
		"        - FOO_TOKEN\n" +
		"        - FOO_OPTIONAL\n" +
		"    primaryEnv: FOO_TOKEN\n" +
		"    install:\n" +
		"      - kind: go\n" +
		"        bins: [foo-pp-cli]\n" +
		"        module: github.com/example/foo/cmd/foo-pp-cli\n"
	got := emitMetadataBlock("foo-pp-cli", "github.com/example/foo/cmd/foo-pp-cli",
		[]string{"FOO_TOKEN", "FOO_OPTIONAL"}, "FOO_TOKEN")
	if got != want {
		t.Errorf("emitMetadataBlock api_key mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestBuildMetadataBlockDefaultsCategoryToOther(t *testing.T) {
	got := buildMetadataBlock("foo-pp-cli", "", "foo", nil, "")
	if !strings.Contains(got, "library/other/foo/cmd/foo-pp-cli") {
		t.Errorf("expected fallback to 'other' category in module path; got:\n%s", got)
	}
}

func TestBuildMetadataBlockWithCategoryAndSlugDir(t *testing.T) {
	// Slug-only directory convention: directory is "dub", binary is "dub-pp-cli".
	got := buildMetadataBlock("dub-pp-cli", "marketing", "dub", nil, "")
	if !strings.Contains(got, "library/marketing/dub/cmd/dub-pp-cli") {
		t.Errorf("expected slug-only directory in module path; got:\n%s", got)
	}
}

func TestBuildMetadataBlockBinarySuffixDirConvention(t *testing.T) {
	// Older binary-suffix convention: directory is "fedex-pp-cli".
	got := buildMetadataBlock("fedex-pp-cli", "commerce", "fedex-pp-cli", nil, "")
	if !strings.Contains(got, "library/commerce/fedex-pp-cli/cmd/fedex-pp-cli") {
		t.Errorf("expected binary-suffix directory in module path; got:\n%s", got)
	}
}

func TestBuildMetadataBlockEmptyDirNameFallsBackToCliName(t *testing.T) {
	// Defensive fallback when caller passes empty dirName.
	got := buildMetadataBlock("x-pp-cli", "other", "", nil, "")
	if !strings.Contains(got, "library/other/x-pp-cli/cmd/x-pp-cli") {
		t.Errorf("expected fallback to cliName when dirName empty; got:\n%s", got)
	}
}

// --- transformMetadataJSON tests ---

func TestTransformMetadataJSON_HappyPath(t *testing.T) {
	jsonStr := `{"openclaw":{"requires":{"bins":["dub-pp-cli"]},"install":[{"id":"go","kind":"shell","command":"go install github.com/mvanhorn/printing-press-library/library/other/dub/cmd/dub-pp-cli@latest","bins":["dub-pp-cli"],"label":"Install via go install"}]}}`
	got, err := transformMetadataJSON(jsonStr, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "kind: go") {
		t.Errorf("output missing kind: go; got:\n%s", got)
	}
	if !strings.Contains(got, "module: github.com/mvanhorn/printing-press-library/library/other/dub/cmd/dub-pp-cli") {
		t.Errorf("output missing expected module path; got:\n%s", got)
	}
	if strings.Contains(got, "kind: shell") {
		t.Errorf("output should not contain kind: shell; got:\n%s", got)
	}
	if strings.Contains(got, "command:") {
		t.Errorf("output should not contain command:; got:\n%s", got)
	}
	if strings.Contains(got, "id:") {
		t.Errorf("output should not contain id:; got:\n%s", got)
	}
	if strings.Contains(got, "label:") {
		t.Errorf("output should not contain label:; got:\n%s", got)
	}
	if strings.Contains(got, "@latest") {
		t.Errorf("module should have @latest stripped; got:\n%s", got)
	}
}

func TestTransformMetadataJSON_WithAuthEnv(t *testing.T) {
	jsonStr := `{"openclaw":{"requires":{"bins":["kalshi-pp-cli"],"env":["KALSHI_TOKEN"]},"primaryEnv":"KALSHI_TOKEN","install":[{"id":"go","kind":"shell","command":"go install github.com/mvanhorn/printing-press-library/library/payments/kalshi/cmd/kalshi-pp-cli@latest","bins":["kalshi-pp-cli"],"label":"Install via go install"}]}}`
	got, err := transformMetadataJSON(jsonStr, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "env:\n        - KALSHI_TOKEN") {
		t.Errorf("output missing env list; got:\n%s", got)
	}
	if !strings.Contains(got, "primaryEnv: KALSHI_TOKEN") {
		t.Errorf("output missing primaryEnv; got:\n%s", got)
	}
}

func TestTransformMetadataJSON_AgentCaptureBareName(t *testing.T) {
	jsonStr := `{"openclaw":{"requires":{"bins":["agent-capture"]},"install":[{"id":"go","kind":"shell","command":"go install github.com/mvanhorn/printing-press-library/library/developer-tools/agent-capture/cmd/agent-capture@latest","bins":["agent-capture"],"label":"Install via go install"}]}}`
	got, err := transformMetadataJSON(jsonStr, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "bins: [agent-capture]") {
		t.Errorf("output missing bare bins; got:\n%s", got)
	}
	if !strings.Contains(got, "/cmd/agent-capture\n") {
		t.Errorf("output module should end with /cmd/agent-capture; got:\n%s", got)
	}
}

func TestTransformMetadataJSON_StrictRejectsBrewKind(t *testing.T) {
	jsonStr := `{"openclaw":{"requires":{"bins":["foo"]},"install":[{"kind":"brew","formula":"foo","bins":["foo"]}]}}`
	_, err := transformMetadataJSON(jsonStr, true)
	if err == nil {
		t.Fatalf("expected strict mode to reject kind: brew")
	}
	if !strings.Contains(err.Error(), "brew") {
		t.Errorf("error message should mention the rejected kind; got: %v", err)
	}
}

func TestTransformMetadataJSON_StrictRejectsNonGoInstallCommand(t *testing.T) {
	jsonStr := `{"openclaw":{"requires":{"bins":["foo"]},"install":[{"kind":"shell","command":"curl -L https://x/y | sh","bins":["foo"]}]}}`
	_, err := transformMetadataJSON(jsonStr, true)
	if err == nil {
		t.Fatalf("expected strict mode to reject non-go-install command")
	}
}

func TestTransformMetadataJSON_MalformedJSON(t *testing.T) {
	_, err := transformMetadataJSON(`{not valid json`, true)
	if err == nil {
		t.Fatalf("expected error on malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse JSON") {
		t.Errorf("error should mention JSON parse failure; got: %v", err)
	}
}

func TestTransformMetadataJSON_MissingBins(t *testing.T) {
	_, err := transformMetadataJSON(`{"openclaw":{"requires":{},"install":[{"kind":"shell","command":"go install x@latest","bins":["x"]}]}}`, true)
	if err == nil {
		t.Fatalf("expected error when requires.bins missing")
	}
}

// --- frontmatterBounds tests ---

func TestFrontmatterBounds(t *testing.T) {
	content := []byte("---\nname: foo\ndescription: bar\n---\n\nbody")
	start, end, err := frontmatterBounds(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := string(content[start:end])
	want := "name: foo\ndescription: bar\n"
	if got != want {
		t.Errorf("frontmatter region mismatch:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFrontmatterBoundsRejectsMissingOpening(t *testing.T) {
	_, _, err := frontmatterBounds([]byte("name: foo\n---\nbody"))
	if err == nil {
		t.Fatalf("expected error on missing opening delimiter")
	}
}

func TestFrontmatterBoundsRejectsMissingClosing(t *testing.T) {
	_, _, err := frontmatterBounds([]byte("---\nname: foo\nbody"))
	if err == nil {
		t.Fatalf("expected error on missing closing delimiter")
	}
}

// --- migrateFile end-to-end tests ---

func TestMigrateFile_LegacyJSONStringConversion(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "library", "other", "dub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	skill := dir + "/SKILL.md"
	original := `---
name: pp-dub
description: "Dub CLI."
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata: '{"openclaw":{"requires":{"bins":["dub-pp-cli"]},"install":[{"id":"go","kind":"shell","command":"go install github.com/mvanhorn/printing-press-library/library/other/dub/cmd/dub-pp-cli@latest","bins":["dub-pp-cli"],"label":"Install via go install"}]}}'
---

# Body content with backticks ` + "`x`" + ` and quotes "y" stays untouched.
`
	if err := os.WriteFile(skill, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	action, err := migrateFile(skill, root, true, false)
	if err != nil {
		t.Fatalf("migrateFile error: %v", err)
	}
	if action != "migrated" {
		t.Errorf("expected action=migrated, got %s", action)
	}
	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "kind: go") {
		t.Error("migrated file should contain kind: go")
	}
	if strings.Contains(string(got), `'{"openclaw":`) {
		t.Error("migrated file should no longer contain JSON-string metadata")
	}
	// Body byte-equal: every line of the original after the metadata line
	// must still be present verbatim.
	if !strings.Contains(string(got), "# Body content with backticks `x` and quotes \"y\" stays untouched.") {
		t.Error("body content with special chars should be preserved byte-equal")
	}
	// The other frontmatter fields stay byte-equal too.
	if !strings.Contains(string(got), `description: "Dub CLI."`) {
		t.Error("description with double quotes should be preserved byte-equal")
	}
}

func TestMigrateFile_Idempotent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "library", "other", "dub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	skill := dir + "/SKILL.md"
	alreadyMigrated := `---
name: pp-dub
description: "Dub CLI."
argument-hint: "<command> [args]"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - dub-pp-cli
    install:
      - kind: go
        bins: [dub-pp-cli]
        module: github.com/x/y/cmd/dub-pp-cli
---

body
`
	if err := os.WriteFile(skill, []byte(alreadyMigrated), 0o644); err != nil {
		t.Fatal(err)
	}
	action, err := migrateFile(skill, root, true, false)
	if err != nil {
		t.Fatalf("migrateFile error: %v", err)
	}
	if action != "skipped" {
		t.Errorf("expected action=skipped on already-nested file, got %s", action)
	}
	got, _ := os.ReadFile(skill)
	if string(got) != alreadyMigrated {
		t.Error("file should be byte-identical after idempotent skip")
	}
}

func TestMigrateFile_DryRunDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "library", "other", "dub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	skill := dir + "/SKILL.md"
	original := `---
name: pp-dub
description: "x"
argument-hint: "<command>"
allowed-tools: "Read Bash"
metadata: '{"openclaw":{"requires":{"bins":["dub-pp-cli"]},"install":[{"id":"go","kind":"shell","command":"go install github.com/x/y/cmd/dub-pp-cli@latest","bins":["dub-pp-cli"],"label":"Install via go install"}]}}'
---

body
`
	if err := os.WriteFile(skill, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	action, err := migrateFile(skill, root, true, true) // dryRun=true
	if err != nil {
		t.Fatalf("migrateFile error: %v", err)
	}
	if action != "migrated" {
		t.Errorf("expected action=migrated even in dry-run, got %s", action)
	}
	got, _ := os.ReadFile(skill)
	if string(got) != original {
		t.Error("dry-run should not modify the file")
	}
}

func TestMigrateFile_SynthesisPath(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "library", "commerce", "instacart")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	provenance := `{"cli_name": "instacart-pp-cli", "category": "commerce"}`
	if err := os.WriteFile(libDir+"/.printing-press.json", []byte(provenance), 0o644); err != nil {
		t.Fatal(err)
	}
	original := `---
name: pp-instacart
description: "Instacart CLI."
argument-hint: "<command>"
allowed-tools: "Read Bash"
---

body
`
	skill := libDir + "/SKILL.md"
	if err := os.WriteFile(skill, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	action, err := migrateFile(skill, root, true, false)
	if err != nil {
		t.Fatalf("migrateFile error: %v", err)
	}
	if action != "synthesized" {
		t.Errorf("expected action=synthesized, got %s", action)
	}
	got, _ := os.ReadFile(skill)
	if !strings.Contains(string(got), "metadata:\n  openclaw:") {
		t.Error("synthesized file should have nested metadata block")
	}
	// Module path uses the slug-only directory ("instacart"), not the cli_name.
	if !strings.Contains(string(got), "module: github.com/mvanhorn/printing-press-library/library/commerce/instacart/cmd/instacart-pp-cli") {
		t.Errorf("synthesized module path wrong; got:\n%s", string(got))
	}
	if !strings.Contains(string(got), "name: pp-instacart") {
		t.Error("original frontmatter fields should be preserved")
	}
	if !strings.Contains(string(got), "\n\nbody\n") {
		t.Error("body content should be preserved")
	}
}

func TestMigrateFile_SynthesisCliSkillsMirror(t *testing.T) {
	root := t.TempDir()
	// Create the library entry that the cli-skills mirror points back to.
	libDir := filepath.Join(root, "library", "commerce", "instacart")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	provenance := `{"cli_name": "instacart-pp-cli", "category": "commerce"}`
	if err := os.WriteFile(libDir+"/.printing-press.json", []byte(provenance), 0o644); err != nil {
		t.Fatal(err)
	}

	mirrorDir := filepath.Join(root, "cli-skills", "pp-instacart")
	if err := os.MkdirAll(mirrorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := `---
name: pp-instacart
description: "Instacart CLI mirror."
argument-hint: "<command>"
allowed-tools: "Read Bash"
---

body
`
	skill := mirrorDir + "/SKILL.md"
	if err := os.WriteFile(skill, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	action, err := migrateFile(skill, root, true, false)
	if err != nil {
		t.Fatalf("migrateFile error: %v", err)
	}
	if action != "synthesized" {
		t.Errorf("expected action=synthesized for cli-skills mirror, got %s", action)
	}
	got, _ := os.ReadFile(skill)
	// Module path uses the library directory basename ("instacart"), not the cli_name or pp-instacart.
	if !strings.Contains(string(got), "module: github.com/mvanhorn/printing-press-library/library/commerce/instacart/cmd/instacart-pp-cli") {
		t.Errorf("mirror synthesis should resolve via library lookup; got:\n%s", string(got))
	}
}

func TestMigrateFile_SynthesisFailsWithoutProvenance(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "library", "commerce", "orphan")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := `---
name: pp-orphan
description: "no metadata, no provenance"
argument-hint: "<command>"
allowed-tools: "Read Bash"
---

body
`
	skill := libDir + "/SKILL.md"
	if err := os.WriteFile(skill, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := migrateFile(skill, root, true, false)
	if err == nil {
		t.Fatalf("expected error when synthesis cannot find provenance")
	}
}

// TestRun_RejectsSymlinkEscapingRoot covers the path-traversal guard. A
// SKILL.md inside the root that's a symlink to a file outside the root
// must fail the EvalSymlinks-then-prefix check. This test fixture also
// exercises the run() orchestrator's error-aggregation path.
func TestRun_RejectsSymlinkEscapingRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideSkill := outside + "/SKILL.md"
	if err := os.WriteFile(outsideSkill, []byte("---\nname: out\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	libDir := filepath.Join(root, "library", "evil", "x")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	insideSymlink := libDir + "/SKILL.md"
	if err := os.Symlink(outsideSkill, insideSymlink); err != nil {
		t.Skipf("symlink not supported on this filesystem: %v", err)
	}

	report, err := run(root, true, true, false)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if report.errored == 0 {
		t.Errorf("expected at least one errored file, got none")
	}
	foundEscape := false
	for _, msg := range report.errors {
		if strings.Contains(msg, "escapes root") {
			foundEscape = true
			break
		}
	}
	if !foundEscape {
		t.Errorf("expected an 'escapes root' error in report; got: %v", report.errors)
	}
}

func TestMigrateFile_BodyJSONShapeIsNotTouched(t *testing.T) {
	// Negative-space test: body content contains JSON-shaped strings; only
	// the frontmatter `metadata:` line should be touched.
	root := t.TempDir()
	dir := filepath.Join(root, "library", "other", "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	skill := dir + "/SKILL.md"
	original := `---
name: pp-x
description: "x"
argument-hint: "<command>"
allowed-tools: "Read Bash"
metadata: '{"openclaw":{"requires":{"bins":["x-pp-cli"]},"install":[{"id":"go","kind":"shell","command":"go install github.com/x/y/cmd/x-pp-cli@latest","bins":["x-pp-cli"],"label":"Install via go install"}]}}'
---

# A recipe block

` + "```bash" + `
echo '{"foo": "bar", "kind": "shell"}'
` + "```" + `

End.
`
	if err := os.WriteFile(skill, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := migrateFile(skill, root, true, false); err != nil {
		t.Fatalf("migrateFile error: %v", err)
	}
	got, _ := os.ReadFile(skill)
	// The body's `kind: shell` (inside the bash block) must NOT be changed.
	if !strings.Contains(string(got), `"kind": "shell"`) {
		t.Error("body's literal JSON shape must be preserved byte-equal; do not touch body content")
	}
	// The frontmatter must use kind: go now.
	if !strings.Contains(string(got), "      - kind: go\n") {
		t.Error("frontmatter should be migrated")
	}
}
