package pipeline

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

// Phase names in execution order.
const (
	PhasePreflight      = "preflight"
	PhaseResearch       = "research"
	PhaseScaffold       = "scaffold"
	PhaseEnrich         = "enrich"
	PhaseRegenerate     = "regenerate"
	PhaseReview         = "review"
	PhaseAgentReadiness = "agent-readiness"
	PhaseComparative    = "comparative"
	PhaseShip           = "ship"
)

// PhaseOrder defines execution order.
var PhaseOrder = []string{
	PhasePreflight,
	PhaseResearch,
	PhaseScaffold,
	PhaseEnrich,
	PhaseRegenerate,
	PhaseReview,
	PhaseAgentReadiness,
	PhaseComparative,
	PhaseShip,
}

// phaseNumber assigns a stable prefix for plan filenames. Numbers use
// gaps (0, 10, 20 …) so future phases can be inserted without renaming
// existing files.
var phaseNumber = map[string]int{
	PhasePreflight:      0,
	PhaseResearch:       10,
	PhaseScaffold:       20,
	PhaseEnrich:         30,
	PhaseRegenerate:     40,
	PhaseReview:         50,
	PhaseAgentReadiness: 55,
	PhaseComparative:    60,
	PhaseShip:           70,
}

// PlanFilename returns the stable plan filename for a phase.
func PlanFilename(phase string) string {
	return fmt.Sprintf("%02d-%s-plan.md", phaseNumber[phase], phase)
}

const (
	StatusPending   = "pending"
	StatusPlanned   = "planned"   // plan.md exists but not yet executed
	StatusExecuting = "executing" // ce:work is running on the plan
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

const (
	PlanStatusSeed      = "seed"
	PlanStatusExpanded  = "expanded"
	PlanStatusCompleted = "completed"
)

// PipelineState tracks which phases are done across sessions.
type PipelineState struct {
	Version                      int                   `json:"version"` // state schema version for migration
	APIName                      string                `json:"api_name"`
	RunID                        string                `json:"run_id,omitempty"`
	Scope                        string                `json:"scope,omitempty"`
	OutputDir                    string                `json:"output_dir"`
	WorkingDir                   string                `json:"working_dir,omitempty"`
	PublishedDir                 string                `json:"published_dir,omitempty"`
	ExcludeFromCurrentResolution bool                  `json:"exclude_from_current_resolution,omitempty"`
	StartedAt                    time.Time             `json:"started_at"`
	Phases                       map[string]PhaseState `json:"phases"`
	SpecPath                     string                `json:"spec_path,omitempty"`
	SpecURL                      string                `json:"spec_url,omitempty"`
	DogfoodTimeout               int                   `json:"dogfood_timeout_seconds,omitempty"` // default 600 (10 min)
	DogfoodTier                  int                   `json:"dogfood_tier,omitempty"`            // max tier to run (1-3, default 1)
}

const currentStateVersion = 3

// PhaseState tracks a single phase.
type PhaseState struct {
	Status     string `json:"status"`
	PlanPath   string `json:"plan_path,omitempty"`
	PlanStatus string `json:"plan_status,omitempty"`
}

type CurrentRunPointer struct {
	APIName    string    `json:"api_name"`
	RunID      string    `json:"run_id"`
	Scope      string    `json:"scope"`
	GitRoot    string    `json:"git_root"`
	WorkingDir string    `json:"working_dir"`
	StatePath  string    `json:"state_path"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// PipelineDir returns the pipeline state directory path.
func PipelineDir(apiName string) string {
	if statePath, ok := resolveStatePath(apiName); ok {
		if filepath.Base(filepath.Dir(statePath)) == "pipeline" {
			return filepath.Dir(statePath)
		}
		return filepath.Join(filepath.Dir(statePath), "pipeline")
	}
	return legacyWorkspacePipelineDir(apiName)
}

func legacyWorkspacePipelineDir(apiName string) string {
	return filepath.Join(LegacyWorkspaceManuscriptsRoot(), apiName, "pipeline")
}

func legacyRepoPipelineDir(apiName string) string {
	return filepath.Join("docs", "plans", apiName+"-pipeline")
}

// StatePath returns the state.json path for an API pipeline.
func StatePath(apiName string) string {
	if statePath, ok := resolveStatePath(apiName); ok {
		return statePath
	}
	return filepath.Join(legacyWorkspacePipelineDir(apiName), "state.json")
}

func legacyWorkspaceStatePath(apiName string) string {
	return filepath.Join(legacyWorkspacePipelineDir(apiName), "state.json")
}

func legacyRepoStatePath(apiName string) string {
	return filepath.Join(legacyRepoPipelineDir(apiName), "state.json")
}

func newRunID(now time.Time) (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generating run id: %w", err)
	}
	return fmt.Sprintf("%s-%x", now.UTC().Format("20060102T150405Z"), suffix), nil
}

func loadCurrentRunPointer(apiName string) (*CurrentRunPointer, error) {
	data, err := os.ReadFile(CurrentRunPointerPath(apiName))
	if err != nil {
		return nil, err
	}

	var pointer CurrentRunPointer
	if err := json.Unmarshal(data, &pointer); err != nil {
		return nil, err
	}
	return &pointer, nil
}

func writeCurrentRunPointer(state *PipelineState) error {
	if state.ExcludeFromCurrentResolution {
		return nil
	}

	pointer := CurrentRunPointer{
		APIName:    state.APIName,
		RunID:      state.RunID,
		Scope:      state.Scope,
		GitRoot:    repoRoot(),
		WorkingDir: state.EffectiveWorkingDir(),
		StatePath:  state.StatePath(),
		UpdatedAt:  time.Now(),
	}

	if err := os.MkdirAll(CurrentRunDir(), 0o755); err != nil {
		return fmt.Errorf("creating current run dir: %w", err)
	}

	data, err := json.MarshalIndent(pointer, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling current run pointer: %w", err)
	}

	return os.WriteFile(CurrentRunPointerPath(state.APIName), data, 0o644)
}

func findRunstateStatePath(apiName string) (string, bool) {
	if pointer, err := loadCurrentRunPointer(apiName); err == nil && pointer.StatePath != "" {
		if _, err := os.Stat(pointer.StatePath); err == nil {
			return pointer.StatePath, true
		}
	}

	pattern := filepath.Join(ScopedRunstateRoot(), "runs", "*", "state.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", false
	}

	var newest string
	var newestTime time.Time
	for _, candidate := range matches {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}

		var state PipelineState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}
		if state.APIName != apiName {
			continue
		}
		if state.ExcludeFromCurrentResolution {
			continue
		}

		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestTime) {
			newest = candidate
			newestTime = info.ModTime()
		}
	}

	if newest == "" {
		return "", false
	}
	return newest, true
}

func resolveStatePath(apiName string) (string, bool) {
	if statePath, ok := findRunstateStatePath(apiName); ok {
		return statePath, true
	}
	if _, err := os.Stat(legacyWorkspaceStatePath(apiName)); err == nil {
		return legacyWorkspaceStatePath(apiName), true
	}
	if _, err := os.Stat(legacyRepoStatePath(apiName)); err == nil {
		return legacyRepoStatePath(apiName), true
	}
	return "", false
}

func LoadCurrentState(apiName string) (*PipelineState, error) {
	pointer, err := loadCurrentRunPointer(apiName)
	if err != nil {
		return nil, fmt.Errorf("no current run for %q", apiName)
	}
	if pointer.StatePath == "" {
		return nil, fmt.Errorf("no current run for %q", apiName)
	}
	if _, err := os.Stat(pointer.StatePath); err != nil {
		return nil, fmt.Errorf("current run for %q is missing state: %w", apiName, err)
	}
	return LoadState(apiName)
}

func FindStateByWorkingDir(dir string) (*PipelineState, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving working dir: %w", err)
	}

	pattern := filepath.Join(ScopedRunstateRoot(), "runs", "*", "state.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("scanning runstate: %w", err)
	}

	for _, candidate := range matches {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}

		var state PipelineState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}
		if state.EffectiveWorkingDir() == absDir {
			if state.RunID == "" {
				state.RunID = filepath.Base(filepath.Dir(candidate))
			}
			if state.Scope == "" {
				state.Scope = WorkspaceScope()
			}
			return &state, nil
		}
	}

	return nil, fmt.Errorf("no runstate entry for working dir %s", absDir)
}

// NewMinimalState creates a lightweight state for CLIs that skipped the
// generate pipeline (e.g. plan-driven CLIs) or whose working directory
// doesn't match any current runstate via FindStateByWorkingDir.
//
// If a runstate exists for this API name in the scoped runstate registry,
// its RunID and Scope are borrowed so downstream consumers (notably
// writeCLIManifestForPublish's research.json enrichment via
// state.PipelineDir()) can locate the run's pipeline artifacts. The
// caller's WorkingDir/OutputDir win over the recovered state's so
// promotion still copies from the correct source. APIName matching is
// enforced by findRunstateStatePath inside resolveStatePath / LoadState;
// this function does not re-validate.
//
// When no recoverable runstate exists, RunID stays empty and downstream
// research.json lookup falls through to its no-op path. The caller can
// observe the recovery decision via state.RunID after construction.
func NewMinimalState(cliName, workingDir string) *PipelineState {
	apiName := naming.TrimCLISuffix(cliName)
	state := &PipelineState{
		Version:    currentStateVersion,
		APIName:    apiName,
		WorkingDir: workingDir,
		OutputDir:  workingDir,
		StartedAt:  time.Now(),
		Phases:     make(map[string]PhaseState),
	}

	// Recover RunID from the scoped runstate registry when possible. A
	// matching state file means a prior generation for this API stored
	// its RunID and pipeline artifacts on disk; downstream enrichment
	// can find them via state.PipelineDir(). On any error (no registry
	// hit, parse failure, IO error) fall through to the empty-RunID
	// minimal state — preserving today's behavior for plan-driven CLIs
	// that genuinely have no prior runstate.
	if loaded, err := LoadState(apiName); err == nil && loaded != nil && loaded.RunID != "" {
		state.RunID = loaded.RunID
		if state.Scope == "" {
			state.Scope = loaded.Scope
		}
	}

	return state
}

// NewState creates a fresh pipeline state.
func NewState(apiName, outputDir string) *PipelineState {
	runID, err := newRunID(time.Now())
	if err != nil {
		runID = fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return NewStateWithRun(apiName, outputDir, runID, WorkspaceScope())
}

func NewManagedState(apiName string) (*PipelineState, error) {
	runID, err := newRunID(time.Now())
	if err != nil {
		return nil, err
	}
	return NewStateWithRun(apiName, WorkingCLIDir(apiName, runID), runID, WorkspaceScope()), nil
}

// NewStateWithRun creates a fresh pipeline state with a preallocated run identity.
func NewStateWithRun(apiName, outputDir, runID, scope string) *PipelineState {
	phases := make(map[string]PhaseState, len(PhaseOrder))
	pipelineDir := RunPipelineDir(runID)
	for _, name := range PhaseOrder {
		phases[name] = PhaseState{
			Status:   StatusPending,
			PlanPath: filepath.Join(pipelineDir, PlanFilename(name)),
		}
	}
	state := &PipelineState{
		Version:        currentStateVersion,
		APIName:        apiName,
		RunID:          runID,
		Scope:          scope,
		OutputDir:      outputDir,
		WorkingDir:     outputDir,
		StartedAt:      time.Now(),
		Phases:         phases,
		DogfoodTimeout: 600, // 10 minutes default
		DogfoodTier:    1,   // default to Tier 1 (no auth)
	}
	return state
}

func (s *PipelineState) save(updateCurrentPointer bool) error {
	if s.Scope == "" {
		s.Scope = WorkspaceScope()
	}
	if s.RunID == "" {
		runID, err := newRunID(time.Now())
		if err != nil {
			return err
		}
		s.RunID = runID
	}
	if s.WorkingDir == "" {
		s.WorkingDir = s.OutputDir
	}
	if s.OutputDir == "" {
		s.OutputDir = s.WorkingDir
	}
	dir := s.PipelineDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating pipeline dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	if err := os.WriteFile(s.StatePath(), data, 0o644); err != nil {
		return err
	}
	if updateCurrentPointer {
		if err := writeCurrentRunPointer(s); err != nil {
			return err
		}
	}
	return nil
}

// Save writes state to disk and updates the current-run pointer for the API.
func (s *PipelineState) Save() error {
	return s.save(true)
}

// SaveWithoutCurrentPointer writes state to disk without taking ownership of
// the API's current-run pointer.
func (s *PipelineState) SaveWithoutCurrentPointer() error {
	return s.save(false)
}

// LoadState reads existing state from disk, migrating old formats.
func LoadState(apiName string) (*PipelineState, error) {
	statePath, ok := resolveStatePath(apiName)
	if !ok {
		return nil, fmt.Errorf("reading state: stat %s: no such file or directory", StatePath(apiName))
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("reading state: %w", err)
	}
	sourcePath := statePath
	var s PipelineState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}

	needsSave := false
	if s.RunID == "" {
		runID, err := newRunID(time.Now())
		if err != nil {
			return nil, err
		}
		s.RunID = runID
		needsSave = true
	}
	if s.Scope == "" {
		s.Scope = WorkspaceScope()
		needsSave = true
	}
	if s.WorkingDir == "" && s.OutputDir != "" {
		s.WorkingDir = s.OutputDir
		needsSave = true
	}
	if s.OutputDir == "" && s.WorkingDir != "" {
		s.OutputDir = s.WorkingDir
		needsSave = true
	}
	// Migrate: add missing phases, update PlanPath to stable-numbered format,
	// and backfill PlanStatus for completed phases that predate the field.
	if s.Version < currentStateVersion {
		for _, name := range PhaseOrder {
			if _, ok := s.Phases[name]; !ok {
				// Backfill missing phases as completed (both Status and PlanStatus)
				// so NextPhase() doesn't treat them as pending.
				s.Phases[name] = PhaseState{
					Status:     StatusCompleted,
					PlanStatus: PlanStatusCompleted,
					PlanPath:   filepath.Join(s.PipelineDir(), PlanFilename(name)),
				}
			} else {
				// Migrate existing phases to stable-numbered PlanPath and
				// backfill PlanStatus for completed phases that predate the field.
				p := s.Phases[name]
				p.PlanPath = filepath.Join(s.PipelineDir(), PlanFilename(name))
				if p.Status == StatusCompleted && p.PlanStatus == "" {
					p.PlanStatus = PlanStatusCompleted
				}
				s.Phases[name] = p
			}
		}
		s.Version = currentStateVersion
		needsSave = true
	}

	if sourcePath != s.StatePath() {
		needsSave = true
	}

	if needsSave {
		if err := s.Save(); err != nil {
			return nil, fmt.Errorf("saving migrated state: %w", err)
		}
	}
	return &s, nil
}

// StateExists returns true if a state file exists.
func StateExists(apiName string) bool {
	_, ok := resolveStatePath(apiName)
	return ok
}

// Start marks a phase as executing.
func (s *PipelineState) Start(phase string) {
	p := s.Phases[phase]
	p.Status = StatusExecuting
	s.Phases[phase] = p
}

// MarkPlanned marks a phase as having its plan.md written.
func (s *PipelineState) MarkPlanned(phase string) {
	p := s.Phases[phase]
	p.Status = StatusPlanned
	s.Phases[phase] = p
}

// MarkSeedWritten marks a phase as having its initial seed plan written.
func (s *PipelineState) MarkSeedWritten(phase string) {
	s.MarkPlanned(phase)
	p := s.Phases[phase]
	p.PlanStatus = PlanStatusSeed
	s.Phases[phase] = p
}

// MarkExpanded marks a phase plan as expanded beyond the initial seed.
func (s *PipelineState) MarkExpanded(phase string) {
	p := s.Phases[phase]
	if p.Status == "" || p.Status == StatusPending {
		p.Status = StatusPlanned
	}
	p.PlanStatus = PlanStatusExpanded
	s.Phases[phase] = p
}

// Complete marks a phase as completed.
func (s *PipelineState) Complete(phase string) {
	p := s.Phases[phase]
	p.Status = StatusCompleted
	p.PlanStatus = PlanStatusCompleted
	s.Phases[phase] = p
}

// CompleteAndPlanNext marks a phase as completed, then generates a dynamic
// plan for the next phase using outputs from all completed phases.
func (s *PipelineState) CompleteAndPlanNext(phase string) error {
	s.Complete(phase)
	nextPhase := s.NextPhase()
	if nextPhase == "" {
		return nil // all phases done
	}

	plan, err := GenerateNextPlan(s, nextPhase)
	if err != nil {
		// Fall back to existing seed plan (already written at init time)
		fmt.Fprintf(os.Stderr, "warning: dynamic plan generation failed for %s, using seed: %v\n", nextPhase, err)
		return nil
	}

	planPath := s.PlanPath(nextPhase)
	if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
		return fmt.Errorf("writing dynamic plan for %s: %w", nextPhase, err)
	}
	s.MarkExpanded(nextPhase)
	return nil
}

// Fail marks a phase as failed.
func (s *PipelineState) Fail(phase string) {
	p := s.Phases[phase]
	p.Status = StatusFailed
	s.Phases[phase] = p
}

func (s *PipelineState) EffectiveWorkingDir() string {
	if s.WorkingDir != "" {
		return s.WorkingDir
	}
	return s.OutputDir
}

func (s *PipelineState) StatePath() string {
	return RunStatePath(s.RunID)
}

func (s *PipelineState) PipelineDir() string {
	return RunPipelineDir(s.RunID)
}

func (s *PipelineState) ResearchDir() string {
	return RunResearchDir(s.RunID)
}

func (s *PipelineState) ProofsDir() string {
	return RunProofsDir(s.RunID)
}

func (s *PipelineState) DiscoveryDir() string {
	return RunDiscoveryDir(s.RunID)
}

func (s *PipelineState) ManifestPath() string {
	return RunManifestPath(s.RunID)
}

// NextPhase returns the name of the next incomplete phase, or "".
func (s *PipelineState) NextPhase() string {
	for _, name := range PhaseOrder {
		if s.Phases[name].PlanStatus != PlanStatusCompleted {
			return name
		}
	}
	return ""
}

// IsComplete returns true if all phases are completed.
func (s *PipelineState) IsComplete() bool {
	return s.NextPhase() == ""
}

// PlanPath returns the plan.md path for a given phase.
func (s *PipelineState) PlanPath(phase string) string {
	return s.Phases[phase].PlanPath
}

// IsSeed reports whether a phase is still at the seed-plan stage.
func (s *PipelineState) IsSeed(phase string) bool {
	return s.Phases[phase].PlanStatus == "" || s.Phases[phase].PlanStatus == PlanStatusSeed
}
