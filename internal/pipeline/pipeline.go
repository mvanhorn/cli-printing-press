package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// DefaultOutputDir returns the default output directory for a given API name.
// All commands should use this when --output is not specified.
func DefaultOutputDir(apiName string) string {
	return filepath.Join(PublishedLibraryRoot(), apiName)
}

// ClaimOutputDir atomically claims an output directory. If base already exists,
// it tries base-2, base-3, ... up to base-99. Uses os.Mkdir (not MkdirAll) for
// the leaf directory so exactly one concurrent caller wins each slot.
func ClaimOutputDir(base string) (string, error) {
	parent := filepath.Dir(base)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("creating parent directory: %w", err)
	}

	// Try the base name first
	err := os.Mkdir(base, 0o755)
	if err == nil {
		return base, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	// Base exists — try -2 through -99
	for i := 2; i <= 99; i++ {
		candidate := base + "-" + strconv.Itoa(i)
		err := os.Mkdir(candidate, 0o755)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("creating output directory: %w", err)
		}
	}

	return "", fmt.Errorf("could not claim output directory: all slots %s through %s-99 are taken", base, base)
}

// Options configures a pipeline run.
type Options struct {
	OutputDir string
	Force     bool
	Resume    bool
	Phase     string
}

// Init creates the pipeline directory, state file, and plan seeds.
// It does NOT execute any phases.
func Init(apiName string, opts Options) (*PipelineState, error) {
	if opts.Resume && StateExists(apiName) {
		return LoadState(apiName)
	}

	runID, err := newRunID(time.Now())
	if err != nil {
		return nil, err
	}
	scope := WorkspaceScope()

	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = WorkingCLIDir(apiName, runID)
	}

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, fmt.Errorf("resolving output dir: %w", err)
	}

	if StateExists(apiName) && !opts.Force {
		return nil, fmt.Errorf("pipeline for %q already exists at %s (use --force to overwrite or --resume to continue)", apiName, PipelineDir(apiName))
	}

	specURL, specSource, err := DiscoverSpec(apiName)
	if err != nil {
		return nil, fmt.Errorf("discovering spec: %w", err)
	}

	state := NewStateWithRun(apiName, absOutputDir, runID, scope)
	state.SpecURL = specURL

	pipeDir := state.PipelineDir()
	if err := os.MkdirAll(pipeDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating pipeline dir: %w", err)
	}

	seedData := SeedData{
		APIName:     apiName,
		OutputDir:   absOutputDir,
		SpecURL:     specURL,
		SpecSource:  specSource,
		PipelineDir: pipeDir,
	}

	// Write only the first two phases as static seeds (preflight + research).
	// Subsequent phases are generated dynamically after prior phases complete.
	staticPhases := []string{PhasePreflight, PhaseResearch}
	for _, phase := range staticPhases {
		content, err := RenderSeed(phase, seedData)
		if err != nil {
			return nil, fmt.Errorf("rendering seed for %s: %w", phase, err)
		}
		planPath := state.PlanPath(phase)
		if err := os.WriteFile(planPath, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("writing plan seed for %s: %w", phase, err)
		}
		state.MarkSeedWritten(phase)
	}

	// For remaining phases, write placeholder plans that will be replaced
	// dynamically when prior phases complete (via CompleteAndPlanNext).
	for _, phase := range PhaseOrder {
		if phase == PhasePreflight || phase == PhaseResearch {
			continue
		}
		content, err := RenderSeed(phase, seedData)
		if err != nil {
			return nil, fmt.Errorf("rendering seed for %s: %w", phase, err)
		}
		planPath := state.PlanPath(phase)
		if err := os.WriteFile(planPath, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("writing plan seed for %s: %w", phase, err)
		}
		state.MarkSeedWritten(phase)
	}

	if err := state.Save(); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	return state, nil
}
