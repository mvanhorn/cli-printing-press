package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"golang.org/x/mod/modfile"
)

// LiveStatus is the outcome of one feature's live check.
type LiveStatus string

const (
	StatusPass                 LiveStatus = "pass"
	StatusFail                 LiveStatus = "fail"
	StatusSkip                 LiveStatus = "skip"
	StatusPrerequisiteUnsynced LiveStatus = "prerequisite_unsynced"
)

// Default bounds for RunLiveCheck. Exported so callers can override via
// LiveCheckOptions without hard-coding magic numbers.
const (
	DefaultLiveCheckTimeout     = 10 * time.Second
	DefaultLiveCheckConcurrency = 4
	// MaxOutputBytes caps the stdout captured from each feature invocation.
	// Relevance matching only needs a few hundred bytes; a 1 MiB cap keeps a
	// misbehaving feature from exhausting the scorecard process's memory.
	MaxOutputBytes = 1 << 20
	// MaxErrorOutputBytes keeps diagnostic stderr captures bounded separately
	// from stdout.
	MaxErrorOutputBytes = 1 << 20
)

// LiveCheckResult summarizes a live-behavior sampling of a printed CLI's
// novel features. For each novel feature with an Example command, the check
// runs the command against real targets (the CLI's actual configured API or
// sites) and asserts the output has the shape a working feature would produce:
// non-empty, query-relevant when the command encodes a query.
//
// Produced by RunLiveCheck. Consumed by the scorecard to apply a behavioral
// correctness cap on the Insight dimension — a Grade A scorecard with a
// flagship feature returning wrong data shouldn't be possible.
type LiveCheckResult struct {
	Passed        int                     `json:"passed"`
	Failed        int                     `json:"failed"`
	Skipped       int                     `json:"skipped"`
	PassRate      float64                 `json:"-"` // exposed via pass_rate_pct in MarshalJSON
	Features      []LiveFeatureResult     `json:"features"`
	BinaryRefresh *LiveCheckBinaryRefresh `json:"binary_refresh,omitempty"`
	Unable        bool                    `json:"unable,omitempty"`
	Reason        string                  `json:"reason,omitempty"`
	RanAt         time.Time               `json:"ran_at"`
}

// Checked returns the total number of features that were sampled.
// Derived; not persisted to avoid the three-counters-for-three-states
// redundancy that makes invariants easy to drift.
func (r *LiveCheckResult) Checked() int {
	if r == nil {
		return 0
	}
	return r.Passed + r.Failed + r.Skipped
}

// Evaluated returns the number of feature samples that reached a pass/fail
// verdict. Skipped samples are reported, but do not count against pass-rate
// math because their prerequisites were not available to live-check.
func (r *LiveCheckResult) Evaluated() int {
	if r == nil {
		return 0
	}
	return r.Passed + r.Failed
}

// LiveFeatureResult is one feature's outcome.
//
// OutputSample carries a bounded snapshot of the captured stdout so
// downstream consumers (Phase 4.85's agentic output review in particular)
// can inspect what the command actually produced without re-invoking the
// CLI. Re-invocation is brittle for stochastic endpoints, expensive for
// rate-limited ones, and auth-dependent for most — persisting a sample
// avoids all three.
//
// Warnings carries advisory findings that don't flip the feature's Status
// — the plan's Wave B ships output-quality checks (like raw HTML entities)
// as warnings for a 2-week calibration window before Wave C escalates them
// to failures. Consumers should surface warnings in reports but not factor
// them into pass-rate math until Wave C lands.
type LiveFeatureResult struct {
	Name         string     `json:"name"`
	Command      string     `json:"command"`
	Example      string     `json:"example"`
	Status       LiveStatus `json:"status"`
	Reason       string     `json:"reason,omitempty"`
	Warnings     []string   `json:"warnings,omitempty"`
	OutputSample string     `json:"output_sample,omitempty"`
}

// LiveCheckBinaryRefresh records whether live-check refreshed the canonical
// staged binary before sampling command examples.
type LiveCheckBinaryRefresh struct {
	Action     string `json:"action"`
	StagePath  string `json:"stage_path,omitempty"`
	BinaryPath string `json:"binary_path,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// outputSampleMaxBytes caps the captured-output snapshot stored on each
// LiveFeatureResult. The raw capture buffer allows up to MaxOutputBytes but
// the serialized sample is bounded much tighter so scorecard JSON files stay
// readable and agentic reviewers don't blow through their context window on
// one feature's output.
const outputSampleMaxBytes = 4096

// sampleRedactionLookaheadBytes lets the redactor see short PII spans that
// start just before the persisted sample cap and end just after it.
const sampleRedactionLookaheadBytes = 512

// LiveCheckOptions bundles the optional knobs for RunLiveCheck. CLIDir is
// required; every other field has a sensible zero-value default.
type LiveCheckOptions struct {
	// CLIDir is the printed CLI's root (which holds the built binary).
	CLIDir string
	// ResearchDir, when non-empty, names a directory that holds research.json
	// instead of CLIDir. Use this when invoking live-check from a working
	// directory the run state owns (where research.json lives next to the
	// run's manuscripts) and the printed CLI hasn't been promoted to its
	// final library location. When blank the live check looks under CLIDir
	// and then walks up a few parent levels (see findResearchDir) so the
	// standard pipeline layout — research.json at the run-dir level, CLI
	// under <runRoot>/working/<api>-pp-cli — works without an explicit
	// override.
	ResearchDir string
	// BinaryName, when non-empty, names the executable to run. Leave blank
	// to let RunLiveCheck derive it from CLIDir (tries `<base>-pp-cli`,
	// falls back to `<base>`).
	BinaryName string
	// Timeout bounds each feature invocation. Zero uses DefaultLiveCheckTimeout.
	Timeout time.Duration
	// Concurrency sets the parallel-feature worker count. Zero uses
	// DefaultLiveCheckConcurrency. Set to 1 to force serial execution.
	Concurrency int
}

// RunLiveCheck samples novel feature Example commands against the real CLI.
// When novel features are absent, it falls back to generated leaf commands
// discovered from agent-context. Returns an Unable=true result (not an error)
// when research.json, the binary, or any sampleable command is missing — the
// scorecard treats those as "could not run" rather than failure, so an absent
// check doesn't penalize the CLI.
func RunLiveCheck(opts LiveCheckOptions) *LiveCheckResult {
	out := &LiveCheckResult{RanAt: time.Now().UTC()}
	if opts.CLIDir == "" {
		out.Unable = true
		out.Reason = "CLIDir is required"
		return out
	}
	releaseHome, err := scopeSubprocessHome(findCLINames(opts.CLIDir)...)
	if err != nil {
		out.Unable = true
		out.Reason = err.Error()
		return out
	}
	defer releaseHome()

	researchDir := opts.ResearchDir
	if researchDir == "" {
		researchDir = findResearchDir(opts.CLIDir)
	}
	research, err := LoadResearch(researchDir)
	if err != nil {
		out.Unable = true
		out.Reason = "no research.json: " + err.Error()
		return out
	}

	refresh, err := refreshLiveCheckStageBinary(opts.CLIDir, opts.BinaryName)
	if refresh.Action != "" {
		out.BinaryRefresh = &refresh
	}
	if err != nil {
		out.Unable = true
		out.Reason = "rebuilding staged binary: " + err.Error()
		return out
	}

	binaryPath, binErr := resolveBinaryPath(opts.CLIDir, opts.BinaryName)
	if binErr != nil {
		out.Unable = true
		out.Reason = binErr.Error()
		return out
	}
	if out.BinaryRefresh != nil {
		out.BinaryRefresh.BinaryPath = binaryPath
	}
	features := pickFeatures(research)
	if len(features) == 0 {
		var fallbackErr error
		features, fallbackErr = pickGeneratedCommandFeatures(binaryPath)
		if fallbackErr != nil {
			out.Unable = true
			out.Reason = "no novel features with Example commands and no generated command fallback: " + fallbackErr.Error()
			return out
		}
		if len(features) == 0 {
			out.Unable = true
			out.Reason = "no novel features with Example commands and no generated command leaves to sample"
			return out
		}
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultLiveCheckTimeout
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultLiveCheckConcurrency
	}
	if concurrency > len(features) {
		concurrency = len(features)
	}

	results := runFeaturesConcurrent(opts.CLIDir, binaryPath, annotateLiveCheckFeatures(opts.CLIDir, features), timeout, concurrency)
	out.Features = results
	for _, r := range results {
		switch r.Status {
		case StatusPass:
			out.Passed++
		case StatusFail:
			out.Failed++
		default:
			out.Skipped++
		}
	}
	if total := out.Evaluated(); total > 0 {
		out.PassRate = float64(out.Passed) / float64(total)
	}
	return out
}

// researchParentWalkDepth bounds how far above CLIDir the live check looks
// for research.json. The standard pipeline lays out
// <runRoot>/working/<api>-pp-cli, putting research.json two levels above
// CLIDir; three is a small margin for layouts that add a wrapper directory
// without inviting scans that could pick up unrelated research.json files
// far above the working tree.
const researchParentWalkDepth = 3

// findResearchDir returns a directory containing research.json that the
// live check can hand to LoadResearch. It first checks cliDir itself, then
// walks up the parent chain up to researchParentWalkDepth levels. If no
// research.json is found, cliDir is returned so the caller's error message
// stays "no research.json: ... <cliDir>/research.json".
//
// The walk handles the canonical non-OpenAPI layout where research.json
// sits at the run-dir level while the printed CLI lives under
// <runRoot>/working/<api>-pp-cli.
func findResearchDir(cliDir string) string {
	if cliDir == "" {
		return cliDir
	}
	dir := cliDir
	for steps := 0; steps <= researchParentWalkDepth; steps++ {
		if _, err := os.Stat(filepath.Join(dir, "research.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cliDir
}

func refreshLiveCheckStageBinary(cliDir, name string) (LiveCheckBinaryRefresh, error) {
	stagePath, stageCandidate := liveCheckExistingStageBinaryPath(cliDir, name)
	if stagePath == "" {
		return LiveCheckBinaryRefresh{Action: "no_stage", Reason: "no staged binary found"}, nil
	}
	refresh := LiveCheckBinaryRefresh{StagePath: stagePath}

	stageInfo, err := os.Stat(stagePath)
	if err != nil {
		refresh.Action = "no_stage"
		refresh.Reason = "staged binary disappeared before stat"
		return refresh, nil
	}

	cmdDir, err := findCLICommandDir(cliDir)
	if err != nil {
		refresh.Action = "skipped"
		refresh.Reason = "no CLI command directory found"
		return refresh, nil
	}

	newestSource, ok, err := newestLiveCheckSourceModTime(cliDir, cmdDir)
	if err != nil {
		refresh.Action = "failed"
		refresh.Reason = err.Error()
		return refresh, err
	}
	if !ok {
		refresh.Action = "skipped"
		refresh.Reason = "no Go source files found"
		return refresh, nil
	}
	if !stageInfo.ModTime().Before(newestSource) {
		refresh.Action = "fresh"
		refresh.Reason = "staged binary is newer than Go sources"
		return refresh, nil
	}
	if freshPath := liveCheckFreshRunnableBinaryPath(cliDir, stageCandidate, newestSource); freshPath != "" {
		refresh.Action = "fresh_fallback"
		refresh.BinaryPath = freshPath
		refresh.Reason = "same-name runnable binary is newer than Go sources"
		return refresh, nil
	}

	if err := rebuildLiveCheckBinary(cliDir, stagePath); err != nil {
		refresh.Action = "failed"
		return refresh, err
	}
	refresh.Action = "rebuilt"
	refresh.BinaryPath = stagePath
	refresh.Reason = "staged binary was older than Go sources"
	return refresh, nil
}

func rebuildLiveCheckBinary(cliDir, binaryPath string) error {
	tmp, err := os.CreateTemp(filepath.Dir(binaryPath), "."+filepath.Base(binaryPath)+".rebuild-*")
	if err != nil {
		return fmt.Errorf("creating binary rebuild temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing binary rebuild temp file: %w", err)
	}
	_ = os.Remove(tmpPath)
	defer func() { _ = os.Remove(tmpPath) }()

	if err := buildCLITo(cliDir, tmpPath); err != nil {
		return err
	}
	if err := replaceLiveCheckBinary(tmpPath, binaryPath); err != nil {
		return fmt.Errorf("replacing binary: %w", err)
	}
	return nil
}

func replaceLiveCheckBinary(src, dst string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(src, dst)
	}
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return os.Rename(src, dst)
	} else if err != nil {
		return err
	}

	backup, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".old-*")
	if err != nil {
		return err
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		_ = os.Remove(backupPath)
		return err
	}
	_ = os.Remove(backupPath)

	if err := os.Rename(dst, backupPath); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		_ = os.Rename(backupPath, dst)
		return err
	}
	_ = os.Remove(backupPath)
	return nil
}

func liveCheckFreshRunnableBinaryPath(cliDir, name string, newestSource time.Time) string {
	for _, candidate := range liveCheckBinaryNames(cliDir, name) {
		for _, path := range liveCheckBinaryCandidatePathsForName(cliDir, candidate, runtime.GOOS) {
			info, err := os.Stat(path)
			if err != nil || !isLiveCheckExecutableForGOOS(path, info.Mode(), runtime.GOOS) {
				continue
			}
			if !info.ModTime().Before(newestSource) {
				return path
			}
		}
	}
	return ""
}

func liveCheckExistingStageBinaryPath(cliDir, name string) (string, string) {
	stagedDir := filepath.Join(cliDir, "build", "stage", "bin")
	for _, candidate := range liveCheckBinaryNames(cliDir, name) {
		for _, path := range liveCheckBinaryCandidatePathsForName(cliDir, candidate, runtime.GOOS) {
			cleanPath := filepath.Clean(path)
			if filepath.Dir(cleanPath) != filepath.Clean(stagedDir) {
				continue
			}
			if _, err := os.Stat(cleanPath); err == nil {
				return cleanPath, candidate
			}
		}
	}
	return "", ""
}

func newestLiveCheckSourceModTime(cliDir, cmdDir string) (time.Time, bool, error) {
	newest, found, err := newestLiveCheckSourceUnder(cliDir)
	if err != nil {
		return time.Time{}, false, err
	}
	mayUseExternal, err := liveCheckMayUseExternalLocalModules(cliDir)
	if err != nil {
		return time.Time{}, false, err
	}
	if !mayUseExternal {
		return newest, found, nil
	}
	graphNewest, graphFound, err := newestLiveCheckBuildGraphModTime(cmdDir)
	if err != nil {
		return time.Time{}, false, err
	}
	if graphFound && (!found || graphNewest.After(newest)) {
		return graphNewest, true, nil
	}
	return newest, found, nil
}

func newestLiveCheckSourceUnder(root string) (time.Time, bool, error) {
	var newest time.Time
	found := false
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		isModuleInput := filepath.Dir(path) == root && (entry.Name() == "go.mod" || entry.Name() == "go.sum")
		if !isModuleInput && (filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go")) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !found || info.ModTime().After(newest) {
			newest = info.ModTime()
			found = true
		}
		return nil
	})
	if err != nil {
		return time.Time{}, false, err
	}
	return newest, found, nil
}

func liveCheckMayUseExternalLocalModules(cliDir string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(cliDir, "go.mod"))
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err == nil {
		file, err := modfile.Parse("go.mod", data, nil)
		if err != nil {
			return false, err
		}
		for _, replacement := range file.Replace {
			path := filepath.FromSlash(replacement.New.Path)
			if replacement.New.Version == "" && (filepath.IsAbs(path) || path == "." || path == ".." || strings.HasPrefix(path, "."+string(os.PathSeparator)) || strings.HasPrefix(path, ".."+string(os.PathSeparator))) {
				return true, nil
			}
		}
	}
	for dir := cliDir; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return false, nil
}

func newestLiveCheckBuildGraphModTime(cmdDir string) (time.Time, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-json", "./"+filepath.Base(cmdDir))
	cmd.Dir = filepath.Dir(cmdDir)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return time.Time{}, false, fmt.Errorf("listing CLI build dependencies: %w\n%s", err, string(exitErr.Stderr))
		}
		return time.Time{}, false, fmt.Errorf("listing CLI build dependencies: %w", err)
	}

	type buildPackage struct {
		Dir          string
		GoFiles      []string
		CgoFiles     []string
		CFiles       []string
		CXXFiles     []string
		MFiles       []string
		HFiles       []string
		FFiles       []string
		SFiles       []string
		SwigFiles    []string
		SwigCXXFiles []string
		SysoFiles    []string
		EmbedFiles   []string
		Module       *struct {
			Main    bool
			Replace *struct {
				Version string
			}
		}
	}

	var newest time.Time
	found := false
	seen := make(map[string]bool)
	decoder := json.NewDecoder(bytes.NewReader(out))
	for {
		var pkg buildPackage
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return time.Time{}, false, fmt.Errorf("decoding CLI build dependencies: %w", err)
		}
		if pkg.Module == nil || (!pkg.Module.Main && (pkg.Module.Replace == nil || pkg.Module.Replace.Version != "")) {
			continue
		}
		files := append([]string{}, pkg.GoFiles...)
		files = append(files, pkg.CgoFiles...)
		files = append(files, pkg.CFiles...)
		files = append(files, pkg.CXXFiles...)
		files = append(files, pkg.MFiles...)
		files = append(files, pkg.HFiles...)
		files = append(files, pkg.FFiles...)
		files = append(files, pkg.SFiles...)
		files = append(files, pkg.SwigFiles...)
		files = append(files, pkg.SwigCXXFiles...)
		files = append(files, pkg.SysoFiles...)
		files = append(files, pkg.EmbedFiles...)
		for _, name := range files {
			path := name
			if !filepath.IsAbs(path) {
				path = filepath.Join(pkg.Dir, path)
			}
			path = filepath.Clean(path)
			if seen[path] {
				continue
			}
			seen[path] = true
			info, err := os.Stat(path)
			if err != nil {
				return time.Time{}, false, err
			}
			if !found || info.ModTime().After(newest) {
				newest = info.ModTime()
				found = true
			}
		}
	}
	return newest, found, nil
}

// resolveBinaryPath returns the absolute path to the CLI binary. When name
// is non-empty it's used verbatim; otherwise RunLiveCheck tries the common
// `<base>-pp-cli` naming convention and falls back to `<base>`.
func resolveBinaryPath(cliDir, name string) (string, error) {
	return resolveBinaryPathForGOOS(cliDir, name, runtime.GOOS)
}

func resolveBinaryPathForGOOS(cliDir, name, goos string) (string, error) {
	candidates := liveCheckBinaryCandidatesForGOOS(cliDir, name, goos)
	var nonExecutablePath string
	for _, candidate := range liveCheckBinaryNames(cliDir, name) {
		var bestPath string
		var bestModTime time.Time
		for _, path := range liveCheckBinaryCandidatePathsForName(cliDir, candidate, goos) {
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if !isLiveCheckExecutableForGOOS(path, info.Mode(), goos) {
				if nonExecutablePath == "" {
					nonExecutablePath = path
				}
				continue
			}
			if bestPath == "" || info.ModTime().After(bestModTime) {
				bestPath = path
				bestModTime = info.ModTime()
			}
		}
		if bestPath != "" {
			absPath, err := filepath.Abs(bestPath)
			if err != nil {
				return "", fmt.Errorf("resolving binary path %q: %w", bestPath, err)
			}
			return absPath, nil
		}
	}
	if nonExecutablePath != "" {
		return "", fmt.Errorf("binary %q is not executable", nonExecutablePath)
	}
	return "", fmt.Errorf("no runnable binary found in %q (tried %v)", cliDir, candidates)
}

func isLiveCheckExecutableForGOOS(path string, mode os.FileMode, goos string) bool {
	if mode.IsDir() {
		return false
	}
	if goos == "windows" {
		return true
	}
	return mode&0o111 != 0
}

func liveCheckBinaryCandidatesForGOOS(cliDir, name, goos string) []string {
	candidates := make([]string, 0)
	seen := map[string]struct{}{}
	for _, candidate := range liveCheckBinaryNames(cliDir, name) {
		for _, path := range liveCheckBinaryCandidatePathsForName(cliDir, candidate, goos) {
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			candidates = append(candidates, path)
		}
	}
	return candidates
}

func liveCheckBinaryNames(cliDir, name string) []string {
	names := []string{name}
	if name == "" {
		base := filepath.Base(cliDir)
		names = []string{base + "-pp-cli", base}
	}
	return names
}

func liveCheckBinaryCandidatePathsForName(cliDir, candidate, goos string) []string {
	// Candidate order breaks ties between equally fresh binaries:
	//   1. <cliDir>/build/stage/bin/<name>           validate-stage Unix
	//   2. <cliDir>/build/stage/bin/<name>.exe       validate-stage Windows
	//   3. <cliDir>/bin/<name>                       Makefile Unix
	//   4. <cliDir>/bin/<name>.exe                   Makefile Windows
	//   5. <cliDir>/<name>                           direct go-build Unix
	//   6. <cliDir>/<name>.exe                       direct go-build Windows
	// The generator's --validate "build runnable binary" gate emits the
	// binary under build/stage/bin/. The generated Makefile writes bin/.
	// Manual fix loops often rebuild directly into cliDir.
	stagedDir := filepath.Join(cliDir, "build", "stage", "bin")
	makefileBinDir := filepath.Join(cliDir, "bin")
	if candidate == "" {
		return nil
	}
	paths := []string{
		filepath.Join(stagedDir, candidate),
		platform.ExecutablePathForGOOS(filepath.Join(stagedDir, candidate), goos),
		filepath.Join(makefileBinDir, candidate),
		platform.ExecutablePathForGOOS(filepath.Join(makefileBinDir, candidate), goos),
		filepath.Join(cliDir, candidate),
		platform.ExecutablePathForGOOS(filepath.Join(cliDir, candidate), goos),
	}
	deduped := paths[:0:0]
	seen := map[string]struct{}{}
	for _, path := range paths {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		deduped = append(deduped, path)
	}
	return deduped
}

type liveCheckFeature struct {
	NovelFeature
	DataSourceStrategy string
}

func annotateLiveCheckFeatures(cliDir string, features []NovelFeature) []liveCheckFeature {
	strategies := novelCommandDataSourceStrategies(cliDir)
	out := make([]liveCheckFeature, 0, len(features))
	for _, f := range features {
		leaf := lastPathSegment(commandPath(f.Command))
		out = append(out, liveCheckFeature{
			NovelFeature:       f,
			DataSourceStrategy: strategies[leaf],
		})
	}
	return out
}

func novelCommandDataSourceStrategies(cliDir string) map[string]string {
	out := map[string]string{}
	cliFilesDir := filepath.Join(cliDir, "internal", "cli")
	entries, err := os.ReadDir(cliFilesDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cliFilesDir, name))
		if err != nil {
			continue
		}
		content := string(data)
		m := dataSourceDirectiveRe.FindStringSubmatch(content)
		if len(m) < 2 {
			continue
		}
		strategy := strings.ToLower(strings.TrimSpace(m[1]))
		if strategy != "auto" && strategy != "local" && strategy != "live" {
			continue
		}
		use := cobraUseLeafRe.FindStringSubmatch(content)
		if len(use) >= 2 {
			out[strings.ToLower(strings.TrimSpace(use[1]))] = strategy
		}
	}
	return out
}

// runFeaturesConcurrent distributes the per-feature checks across a worker
// pool. Results are collected in-order so LiveCheckResult.Features stays
// stable across runs.
func runFeaturesConcurrent(cliDir, binaryPath string, features []liveCheckFeature, timeout time.Duration, concurrency int) []LiveFeatureResult {
	results := make([]LiveFeatureResult, len(features))
	type job struct{ idx int }
	jobs := make(chan job, len(features))
	for i := range features {
		jobs <- job{idx: i}
	}
	close(jobs)

	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			for j := range jobs {
				results[j.idx] = runOneFeatureCheckWithDataSource(cliDir, binaryPath, features[j.idx], timeout)
			}
		})
	}
	wg.Wait()
	return results
}

// pickFeatures returns the novel features to sample. Prefers NovelFeaturesBuilt
// (the verified subset) when dogfood has run; falls back to NovelFeatures.
// Only features with a non-empty Example are usable — the rest are skipped
// silently since we have no invocation to run.
func pickFeatures(r *ResearchResult) []NovelFeature {
	source := r.NovelFeatures
	if r.NovelFeaturesBuilt != nil && len(*r.NovelFeaturesBuilt) > 0 {
		source = *r.NovelFeaturesBuilt
	}
	var out []NovelFeature
	for _, f := range source {
		if strings.TrimSpace(f.Example) != "" {
			out = append(out, f)
		}
	}
	return out
}

func pickGeneratedCommandFeatures(binaryPath string) ([]NovelFeature, error) {
	out, err := runStdoutOnly(binaryPath, 15*time.Second, "agent-context")
	if err != nil {
		return nil, fmt.Errorf("agent-context failed: %w", err)
	}
	paths, err := dogfoodExampleCommandPathsFromAgentContext(out)
	if err != nil {
		return nil, err
	}
	if len(paths) > 5 {
		paths = sampleEvenlyCommandPaths(paths, 5)
	}
	binaryName := filepath.Base(binaryPath)
	features := make([]NovelFeature, 0, len(paths))
	for _, path := range paths {
		command := strings.Join(path, " ")
		features = append(features, NovelFeature{
			Name:        command,
			Command:     command,
			Description: "Generated command " + command,
			Example:     binaryName + " " + command + " --json",
		})
	}
	return features, nil
}

// runOneFeatureCheck parses the Example invocation, runs it against the real
// binary, and evaluates the output shape. The Example is expected to start
// with the binary name (e.g., "recipe-goat-pp-cli goat \"brownies\" --limit 5");
// we drop that prefix and replace it with the absolute binary path so the
// check works regardless of the caller's PATH.
//
// Note: runCLIWithOutput in runtime.go uses CombinedOutput (stdout+stderr
// merged) and wraps non-zero exits as a generic error. This check instead
// separates stdout (for the relevance pass) from stderr (for failure
// messaging) and needs structured access to *exec.ExitError +
// DeadlineExceeded, so it runs exec inline.
func runOneFeatureCheck(cliDir, binaryPath string, f NovelFeature, timeout time.Duration) LiveFeatureResult {
	return runOneFeatureCheckWithDataSource(cliDir, binaryPath, liveCheckFeature{NovelFeature: f}, timeout)
}

func runOneFeatureCheckWithDataSource(cliDir, binaryPath string, f liveCheckFeature, timeout time.Duration) LiveFeatureResult {
	result := LiveFeatureResult{Name: f.Name, Command: f.Command, Example: f.Example}
	fail := func(reason string) LiveFeatureResult {
		result.Status = StatusFail
		result.Reason = reason
		return result
	}

	args, err := parseExampleArgs(f.Example)
	if err != nil {
		result.Status = StatusSkip
		result.Reason = "could not parse example: " + err.Error()
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = cliDir
	applyDefaultSubprocessEnv(cmd)
	// Capture stdout into a bounded buffer. An unbounded `cmd.Output()` call
	// would let a misbehaving feature exhaust the scorecard's memory.
	stdoutCap := &bytes.Buffer{}
	stderrCap := &bytes.Buffer{}
	cmd.Stdout = &limitedWriter{w: stdoutCap, remaining: MaxOutputBytes}
	cmd.Stderr = &limitedWriter{w: stderrCap, remaining: MaxErrorOutputBytes}
	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fail(fmt.Sprintf("timed out after %s", timeout))
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			stderr := stderrCap.String()
			if f.DataSourceStrategy == "local" && isUnsyncedLocalStoreFailure(stderr) {
				result.Status = StatusPrerequisiteUnsynced
				result.Reason = "prerequisite_unsynced: local store is not populated; run sync before probing this command"
				return result
			}
			if isGracefulEmptyResponse(stderr, args) {
				// CLI exited non-zero gracefully on "no record matches this
				// input" — that's the CORRECT behavior for an unknown slug
				// (research.json's LLM-authored example slugs decay over
				// time on content-rotating APIs). Don't penalize a feature
				// that handled the empty case well. See issue #484.
				result.Status = StatusPass
				result.Reason = "graceful empty: " + trimOutput(stderr)
				result.OutputSample = sampleOutput(stderr)
				return result
			}
			return fail(fmt.Sprintf("exit %d: %s", exitErr.ExitCode(), trimOutput(stderr)))
		}
		return fail("run error: " + runErr.Error())
	}
	if strings.TrimSpace(stdoutCap.String()) == "" {
		return fail("empty output")
	}

	if query := extractQueryToken(args, f.Command); query != "" {
		if !outputMentionsQuery(stdoutCap.String(), query) {
			return fail(fmt.Sprintf("output does not contain any token from query %q", query))
		}
		if outputIsBareQueryEcho(stdoutCap.String(), query) {
			return fail(fmt.Sprintf("output only echoes query %q without result structure", query))
		}
	}

	stdout := stdoutCap.String()
	result.Status = StatusPass
	result.OutputSample = sampleOutput(stdout)
	if msg := detectRawHTMLEntities(stdout, args); msg != "" {
		result.Warnings = append(result.Warnings, msg)
	}
	return result
}

func isUnsyncedLocalStoreFailure(stderr string) bool {
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "unable to open database file") ||
		strings.Contains(lower, "no such file or directory")
}

// sampleOutput truncates captured output to outputSampleMaxBytes for
// persistence on LiveFeatureResult.OutputSample. An ellipsis marker at the
// boundary tells downstream readers the snapshot is truncated.
func sampleOutput(s string) string {
	return sampleOutputParts(s)
}

func sampleOutputParts(parts ...string) string {
	var rawSample strings.Builder
	captureRemaining := outputSampleMaxBytes + sampleRedactionLookaheadBytes
	capRemaining := outputSampleMaxBytes
	truncated := false
	for _, part := range parts {
		part = artifacts.RedactPIIJWTs(part)
		if redacted, ok := artifacts.RedactPIIJSONKeys(part); ok {
			part = redacted
		}
		if len(part) > capRemaining {
			truncated = true
		}
		if capRemaining > 0 {
			if len(part) >= capRemaining {
				capRemaining = 0
			} else {
				capRemaining -= len(part)
			}
		}
		if captureRemaining <= 0 {
			continue
		}
		if len(part) > captureRemaining {
			rawSample.WriteString(truncateUTF8(part, captureRemaining))
			captureRemaining = 0
			continue
		}
		rawSample.WriteString(part)
		captureRemaining -= len(part)
	}
	sample := artifacts.RedactPIIText(rawSample.String())
	if truncated || captureRemaining == 0 {
		sample = redactPartialJWTTail(sample)
	}
	if len(sample) > outputSampleMaxBytes {
		sample = truncateUTF8(sample, outputSampleMaxBytes)
		sample = completePartialRedactionSentinel(sample)
		truncated = true
	}
	if truncated {
		return sample + "…[truncated]"
	}
	return sample
}

var partialJWTTailRE = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]*(?:\.[A-Za-z0-9_-]*){0,2}$`)

func redactPartialJWTTail(sample string) string {
	return partialJWTTailRE.ReplaceAllString(sample, artifacts.PIIRedactedSentinel)
}

func completePartialRedactionSentinel(sample string) string {
	const partialSentinelPrefix = "<redact"
	idx := strings.LastIndex(sample, partialSentinelPrefix)
	if idx == -1 || strings.Contains(sample[idx:], artifacts.PIIRedactedSentinel) {
		return sample
	}
	return sample[:idx] + artifacts.PIIRedactedSentinel
}

func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:maxBytes])
		if r != utf8.RuneError || size != 1 {
			break
		}
		maxBytes--
	}
	return s[:maxBytes]
}

// rawHTMLEntityRe matches numeric HTML character references, both decimal
// (&#39;) and hex (&#x27;). Named entities (&amp;, &quot;, &lt;) are
// intentionally excluded in Wave B — they false-positive on legitimate JSON
// strings ("AT&amp;T") and on documentation text, so we calibrate the
// stricter numeric-only rule first. Wave C may broaden to named entities
// after observing the library's actual output patterns.
//
// The digit count is bounded at 10 to prevent a malicious output like
// "&#99999999999...;" from forcing the regex engine into a large matched
// span that then propagates into the warning message. Valid Unicode code
// points fit in 7 decimal digits (10FFFF = 1,114,111), so 10 is a generous
// upper bound that stays well inside regex budget.
var rawHTMLEntityRe = regexp.MustCompile(`&#[xX]?[0-9a-fA-F]{1,10};`)

// detectRawHTMLEntities returns a short human-readable reason when output
// contains raw numeric HTML entities, or "" when output is clean. Gated to
// non-JSON output so JSON-mode features (whose output legitimately contains
// escape sequences) don't trip the check.
//
// JSON-mode detection:
//   - any arg that is `--json` or begins with `--json=` (cobra accepts both)
//   - first non-whitespace character of output is `{` or `[`
//
// Both heuristics are conservative: a feature that renders JSON inside a
// human table would still be checked, which is the right behavior.
func detectRawHTMLEntities(output string, args []string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	// Skip JSON-mode: agent-facing output legitimately contains escape
	// sequences and isn't rendered to a human terminal.
	for _, a := range args {
		if a == "--json" || strings.HasPrefix(a, "--json=") {
			return ""
		}
	}
	if first := trimmed[0]; first == '{' || first == '[' {
		return ""
	}
	match := rawHTMLEntityRe.FindString(output)
	if match == "" {
		return ""
	}
	// Cap the match-echo so a pathological output (e.g., a huge
	// numeric ref — guarded by the regex's 10-digit bound, but the
	// warning message still benefits from a defensive cap) can't
	// produce megabyte-sized warning strings in the scorecard JSON.
	if len(match) > 64 {
		match = match[:64] + "…"
	}
	return fmt.Sprintf("raw HTML entity %q in output (decode with cliutil.CleanText or equivalent)", match)
}

// limitedWriter caps the bytes forwarded to w at `remaining`; further writes
// are discarded (but still report as successful, so the subprocess doesn't
// SIGPIPE). Intentionally tolerant of truncation — the live check only needs
// enough output to run a relevance match.
type limitedWriter struct {
	w         io.Writer
	remaining int
	truncated bool
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.remaining <= 0 {
		if len(p) > 0 {
			lw.truncated = true
		}
		return len(p), nil
	}
	n := min(len(p), lw.remaining)
	if _, err := lw.w.Write(p[:n]); err != nil {
		return 0, err
	}
	lw.remaining -= n
	if n < len(p) {
		lw.truncated = true
	}
	return len(p), nil
}

// parseExampleArgs takes an Example like:
//
//	recipe-goat-pp-cli goat "chicken tikka masala" --limit 5
//
// and returns the subcommand arguments (everything after the binary name).
// Respects double-quoted tokens so queries with spaces stay intact.
func parseExampleArgs(example string) ([]string, error) {
	return shellargs.ArgsAfterBinary(example)
}

// extractQueryToken returns a positional argument that looks like a human-
// readable search query — the kind of token a search/filter command would
// mention in its output. Returns "" when no such argument exists, in which
// case no relevance check is performed.
//
// URLs and IDs are intentionally excluded: the CLI's output for a URL-based
// command (recipe get <url>) wouldn't contain the URL itself, so matching
// against it produces false negatives. commandPath is the cobra command
// path being exercised (e.g. "leaderboard top"); positionals that match a
// word from it are treated as subcommand names, not queries — without
// this, `leaderboard top` would treat `top` as a search query and fail
// the relevance heuristic against output that has no reason to echo it.
//
// Examples:
//
//	args=["goat", "brownies", "--limit", "5"], cmd="goat"           → "brownies"
//	args=["sub", "buttermilk"], cmd="sub"                            → "buttermilk"
//	args=["recipe", "get", "https://foo/bar"], cmd="recipe get"      → "" (URL, skip)
//	args=["cookbook", "list", "--json"], cmd="cookbook list"         → "" (no query)
//	args=["leaderboard", "top"], cmd="leaderboard top"               → "" (subcommand name)
func extractQueryToken(args []string, commandPath string) string {
	var positionals []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		positionals = append(positionals, arg)
	}
	if len(positionals) < 2 {
		return ""
	}
	candidate := positionals[len(positionals)-1]
	candidateLower := strings.ToLower(candidate)
	for word := range strings.FieldsSeq(strings.ToLower(commandPath)) {
		if word == candidateLower {
			return ""
		}
	}
	if looksLikeURLOrID(candidate) {
		return ""
	}
	if commonCommandVerb(candidate) {
		return ""
	}
	if len(candidate) < 3 {
		return ""
	}
	return candidate
}

func commonCommandVerb(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	// CRUD / action verbs.
	case "list", "get", "show", "fetch", "query", "search", "create", "update", "delete", "remove", "set", "run":
		return true
	// Time / state words used as command names rather than user-supplied
	// queries. Commands like `slices today`, `store tonight`, `events now`
	// are verb-shaped: the trailing word names the view, not a search term.
	// Without this branch, the rendered output (which has no reason to
	// echo "today" as text — it's structured data) would fail the
	// outputMentionsQuery heuristic and produce a spurious live-check
	// failure on otherwise-correct output.
	case "today", "tonight", "now", "current", "latest", "recent", "upcoming",
		"pending", "active", "expired", "stale", "all", "live", "open", "closed":
		return true
	default:
		return false
	}
}

// looksLikeURLOrID returns true for tokens that shouldn't be used as search-
// relevance queries: URLs, numeric IDs, UUIDs. The CLI output for a
// get-by-id command won't contain the ID as text.
func looksLikeURLOrID(s string) bool {
	if strings.Contains(s, "://") || strings.HasPrefix(s, "/") {
		return true
	}
	allDigits := true
	for _, c := range s {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	return allDigits
}

// outputMentionsQuery is case-insensitive; splits the query on whitespace
// and commas, then succeeds if any token (with singular/plural tolerance)
// appears in the output. Mirrors the permissive relevance check used
// inside generated CLIs. Splitting on commas catches comma-separated
// list queries like `pikachu,charizard,blastoise` whose individual names
// appear in JSON-array outputs separated by quote+comma+quote.
func outputMentionsQuery(output, query string) bool {
	lowered := strings.ToLower(output)
	splitOnQueryDelim := func(r rune) bool { return unicode.IsSpace(r) || r == ',' }
	for _, tok := range strings.FieldsFunc(strings.ToLower(query), splitOnQueryDelim) {
		tok = strings.TrimFunc(tok, func(r rune) bool { return r == '"' || r == '\'' })
		if len(tok) < 3 {
			continue
		}
		if strings.Contains(lowered, tok) {
			return true
		}
		if strings.HasSuffix(tok, "s") && len(tok) > 3 {
			if strings.Contains(lowered, tok[:len(tok)-1]) {
				return true
			}
		}
	}
	return false
}

func outputIsBareQueryEcho(output, query string) bool {
	trimmed := strings.TrimSpace(output)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return false
	}
	outputWords := normalizedOutputWords(output)
	queryWords := normalizedOutputWords(query)
	if len(outputWords) == 0 || len(queryWords) == 0 {
		return false
	}
	if len(outputWords) != len(queryWords) {
		return false
	}
	for i := range outputWords {
		if outputWords[i] != queryWords[i] {
			return false
		}
	}
	return true
}

func normalizedOutputWords(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return fields
}

func trimOutput(s string) string {
	s = artifacts.RedactPIIText(strings.TrimSpace(s))
	if len(s) > 300 {
		s = truncateUTF8(s, 300) + "..."
	}
	return s
}

// gracefulEmptyPhrases is the closed vocabulary of stderr substrings that
// signal "the CLI looked up the user's input and found no matching record."
// Kept short and explicit to avoid false positives on generic error prose.
// Lower-cased; matched case-insensitively against stderr.
var gracefulEmptyPhrases = []string{
	"not found",
	"no results",
	"no match",
	"no record",
	"no such",
}

// isGracefulEmptyResponse reports whether a non-zero exit + stderr indicates
// the CLI gracefully handled an "input maps to no record" case rather than
// hit an actual bug. Two conditions must both hold:
//
//  1. stderr contains one of gracefulEmptyPhrases.
//  2. stderr echoes at least one of the user-supplied positional args
//     (≥ 3 chars). This is the key boundary — it filters out generic
//     failures like "config file not found" or "authentication required"
//     that happen to contain a graceful phrase but don't reference user
//     input. Flag values written as "--key value" count as positional
//     candidates because a CLI echoing a flag's value (e.g. --query notion
//     → "no match for query: notion") is still strong evidence that user
//     input was processed.
//
// Caller contract: invoke only on non-zero exits. See issue #484.
func isGracefulEmptyResponse(stderr string, args []string) bool {
	lower := strings.ToLower(stderr)
	if !containsAnyOf(lower, gracefulEmptyPhrases) {
		return false
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		a := strings.ToLower(arg)
		if len(a) < 3 {
			// Skip short tokens to avoid coincidental substring matches.
			// Three chars is the same threshold outputMentionsQuery uses.
			continue
		}
		if strings.Contains(lower, a) {
			return true
		}
	}
	return false
}

// containsAnyOf reports whether any of needles is a substring of s. The
// "any-of" suffix distinguishes this from dogfood.go's containsAny, which
// has the inverse signature ([]string sources, string needle). Caller is
// expected to pass a pre-lowered s when matching case-insensitively.
func containsAnyOf(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// InsightCapFromLiveCheck returns the maximum Insight score a CLI should
// receive given its live-check pass rate. A CLI whose flagships return
// broken output shouldn't earn a Grade A scorecard.
//
//   - Unable or zero evaluated samples: no cap (nil return)
//   - PassRate >= 0.8: no cap
//   - PassRate >= 0.5: cap at 7
//   - PassRate <  0.5: cap at 4
func InsightCapFromLiveCheck(r *LiveCheckResult) *int {
	if r == nil || r.Unable || r.Evaluated() == 0 {
		return nil
	}
	var cap int
	switch {
	case r.PassRate >= 0.8:
		return nil
	case r.PassRate >= 0.5:
		cap = 7
	default:
		cap = 4
	}
	return &cap
}

// MarshalJSON emits a rounded pass_rate_pct alongside the raw counters so
// JSON consumers don't have to deal with floating-point noise. PassRate is
// hidden via json:"-" on the struct; this method computes the percentage
// once using an alias to avoid infinite recursion.
func (r *LiveCheckResult) MarshalJSON() ([]byte, error) {
	type alias LiveCheckResult
	return json.Marshal(&struct {
		*alias
		Checked     int `json:"checked"`
		Evaluated   int `json:"evaluated"`
		PassRatePct int `json:"pass_rate_pct"`
	}{
		alias:       (*alias)(r),
		Checked:     r.Checked(),
		Evaluated:   r.Evaluated(),
		PassRatePct: int(r.PassRate*100 + 0.5),
	})
}
