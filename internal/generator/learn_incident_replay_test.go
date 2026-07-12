package generator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// TestIncidentReplay is the U17 incident-replay harness: it generates a
// learn-enabled CLI once, builds the real binary once, and scripts the
// plan's acceptance examples end to end against it, driving separate
// processes with isolated HOME/state dirs and per-session
// <PREFIX>_LEARN_SESSION keys. Each acceptance example is a subtest.
//
// The subtests use require for plumbing that later steps depend on and
// assert for the acceptance-contract clauses themselves, so one run
// reports every contract gap instead of stopping at the first.
func TestIncidentReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("incident replay harness builds and drives a generated binary; skipped in -short mode")
	}

	apiSpec := incidentReplaySpec("incident-replay")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	binName := naming.CLI(apiSpec.Name)
	runGoCommand(t, outputDir, "build", "-o", binName, "./cmd/"+binName)

	server := httptest.NewServer(http.HandlerFunc(incidentReplayAPI))
	defer server.Close()

	h := &incidentHarness{
		t:         t,
		binary:    filepath.Join(outputDir, binName),
		binName:   binName,
		envPrefix: naming.EnvPrefix(apiSpec.Name),
		baseURL:   server.URL,
		counts:    map[string]int{},
	}

	// AE2 runs against its own state dir, so it never observes the
	// confirms AE1 performs; ordering between subtests is therefore
	// immaterial, but they stay sequential (no t.Parallel) because the
	// pid-lineage subtest depends on every child process sharing this
	// test process as its parent.
	t.Run("AE1_incident_replay_with_confirm", h.testAE1IncidentReplayWithConfirm)
	t.Run("AE2_no_confirm_steady_state", h.testAE2NoConfirmSteadyState)
	t.Run("AE3_reject_recovery", h.testAE3RejectRecovery)
	t.Run("AE4_phrasing_convergence", h.testAE4PhrasingConvergence)
	t.Run("AE6_harness_silence", h.testAE6HarnessSilence)
	t.Run("SessionKeyFallback_pid_lineage", h.testSessionKeyFallbackPidLineage)
}

// incidentReplaySpec is minimalSpec plus a second endpoint so the
// scripted discovery has more than one command shape to explore, the
// way a real agent session does.
func incidentReplaySpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Learn.Enabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/items", Description: "List items"},
				"get": {
					Method:      "GET",
					Path:        "/items/{id}",
					Description: "Get one item",
					Params: []spec.Param{
						{Name: "id", Type: "string", Required: true, PathParam: true},
					},
				},
			},
		},
	}
	return apiSpec
}

// incidentReplayAPI is the loopback API the generated CLI talks to, so
// every scripted invocation stays hermetic (no external network).
func incidentReplayAPI(w http.ResponseWriter, r *http.Request) {
	items := []map[string]string{
		{"id": "alpha-recap", "name": "Alphas recap", "winner": "Alphas", "score": "3-1"},
		{"id": "bravo-recap", "name": "Bravos recap", "winner": "Bravos", "score": "2-0"},
	}
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "/items" {
		_ = json.NewEncoder(w).Encode(items)
		return
	}
	if id, ok := strings.CutPrefix(path, "/items/"); ok {
		for _, item := range items {
			if item["id"] == id {
				_ = json.NewEncoder(w).Encode(item)
				return
			}
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

// incidentHarness drives the built binary as separate processes with a
// controlled environment and counts invocations per session key so the
// AE1 efficiency gate compares real numbers.
type incidentHarness struct {
	t         *testing.T
	binary    string
	binName   string
	envPrefix string
	baseURL   string
	counts    map[string]int
}

// run executes one CLI invocation. The environment is built from
// scratch (never inherited) so PRINTING_PRESS_VERIFY and any ambient
// session key can never leak in; sessionKey "" means no
// <PREFIX>_LEARN_SESSION var at all, exercising the pid-lineage
// fallback. Every invocation increments the per-session counter.
func (h *incidentHarness) run(home, sessionKey string, verifyEnv bool, args ...string) (stdout, stderr string, exitCode int) {
	h.t.Helper()
	h.counts[sessionKey]++

	cmd := exec.Command(h.binary, args...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"TMPDIR=" + os.TempDir(),
		h.envPrefix + "_HOME=" + home,
		h.envPrefix + "_BASE_URL=" + h.baseURL,
		"MYAPI_TOKEN=dummy-token-for-harness",
	}
	if sessionKey != "" {
		cmd.Env = append(cmd.Env, h.envPrefix+"_LEARN_SESSION="+sessionKey)
	}
	if verifyEnv {
		cmd.Env = append(cmd.Env, "PRINTING_PRESS_VERIFY=1")
	}
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		require.True(h.t, ok, "invocation %v failed to run: %v", args, err)
		exitCode = exitErr.ExitCode()
	}
	return out.String(), errBuf.String(), exitCode
}

// Envelope shapes for the slices of the recall JSON the harness
// asserts on. Field names pin the agent-visible JSON contract.
type incidentRecallEnvelope struct {
	Found      bool                   `json:"found"`
	Normalized string                 `json:"normalized"`
	Warnings   []string               `json:"warnings"`
	Notes      string                 `json:"notes"`
	Playbook   *incidentResolvedPB    `json:"playbook"`
	Candidates []incidentEnvelopeCand `json:"candidates"`
	Results    []map[string]any       `json:"results"`
}

type incidentResolvedPB struct {
	QueryFamily string `json:"query_family"`
	Confidence  int    `json:"confidence"`
	Notes       string `json:"notes"`
	Playbook    struct {
		Steps []struct {
			Cmd string `json:"cmd"`
		} `json:"steps"`
	} `json:"playbook"`
}

type incidentEnvelopeCand struct {
	ID         int64    `json:"id"`
	Class      string   `json:"class"`
	Sightings  int      `json:"sightings"`
	NextAction []string `json:"next_action"`
}

// incidentCandidateRow mirrors the `learnings candidates --json` row
// contract (candidateRow in the emitted internal/cli).
type incidentCandidateRow struct {
	ID          int64           `json:"id"`
	Class       string          `json:"class"`
	Status      string          `json:"status"`
	Sightings   int             `json:"sightings"`
	QueryFamily string          `json:"query_family"`
	CommandPath string          `json:"command_path"`
	Payload     json.RawMessage `json:"payload"`
}

// incidentJournalEntry mirrors the emitted JournalEntry JSON shape.
type incidentJournalEntry struct {
	SessionKey    string            `json:"session_key"`
	Cmd           []string          `json:"cmd"`
	ArgvShape     map[string]string `json:"argv_shape"`
	ExitCode      int               `json:"exit_code"`
	FailedFlag    string            `json:"failed_flag"`
	SuggestedFlag string            `json:"suggested_flag"`
	QueryFamily   string            `json:"query_family"`
}

func (h *incidentHarness) recallEnvelope(home, sessionKey, query string) incidentRecallEnvelope {
	h.t.Helper()
	stdout, stderr, exit := h.run(home, sessionKey, false, "recall", query, "--json")
	require.Equal(h.t, 0, exit, "recall %q must exit 0; stderr:\n%s", query, stderr)
	var env incidentRecallEnvelope
	require.NoError(h.t, json.Unmarshal([]byte(stdout), &env), "recall envelope must be JSON; got:\n%s", stdout)
	return env
}

// listCandidates inspects the candidate store through the control
// command. status "" lists every row regardless of state.
func (h *incidentHarness) listCandidates(home, sessionKey, status string) []incidentCandidateRow {
	h.t.Helper()
	stdout, stderr, exit := h.run(home, sessionKey, false, "learnings", "candidates", "--status", status, "--json")
	require.Equal(h.t, 0, exit, "learnings candidates must exit 0; stderr:\n%s", stderr)
	var rows []incidentCandidateRow
	require.NoError(h.t, json.Unmarshal([]byte(stdout), &rows), "candidates listing must be JSON; got:\n%s", stdout)
	return rows
}

func (h *incidentHarness) readJournal(home string) []incidentJournalEntry {
	h.t.Helper()
	segments, err := filepath.Glob(filepath.Join(home, "state", "learn", "journal-*.jsonl"))
	require.NoError(h.t, err)
	var entries []incidentJournalEntry
	for _, seg := range segments {
		data, err := os.ReadFile(seg)
		require.NoError(h.t, err)
		for line := range strings.SplitSeq(string(data), "\n") {
			if !strings.HasPrefix(line, "{") {
				continue
			}
			var e incidentJournalEntry
			require.NoError(h.t, json.Unmarshal([]byte(line), &e), "journal line must parse: %s", line)
			entries = append(entries, e)
		}
	}
	return entries
}

func (h *incidentHarness) journalFor(home, sessionKey string) []incidentJournalEntry {
	h.t.Helper()
	var out []incidentJournalEntry
	for _, e := range h.readJournal(home) {
		if e.SessionKey == sessionKey {
			out = append(out, e)
		}
	}
	return out
}

// runIncident scripts the canonical failure incident under one session
// key: fail a flag that exists nowhere in the command tree (--selec,
// which draws the did-you-mean --select hint), then rerun corrected.
func (h *incidentHarness) runIncident(home, sessionKey string) {
	h.t.Helper()
	_, stderr, exit := h.run(home, sessionKey, false, "items", "list", "--selec", "winner", "--json")
	require.Equal(h.t, 2, exit, "unknown-flag failure must exit 2 (usage)")
	require.Contains(h.t, stderr, "did you mean --select", "the did-you-mean hint must fire for --selec")
	_, stderr, exit = h.run(home, sessionKey, false, "items", "list", "--select", "winner", "--json")
	require.Equal(h.t, 0, exit, "corrected retry must succeed; stderr:\n%s", stderr)
}

const (
	incidentFamilyQueryA   = "why did the Alphas win yesterday"
	incidentFamilyQueryB   = "why did the Bravos win yesterday"
	incidentExpectedFamily = "win yesterday"
)

// testAE1IncidentReplayWithConfirm enforces AE1: session A pays the
// full discovery cost (recall miss, exploration, a failed flag, the
// corrected retry, the answer, a background teach); session B on the
// same query family with a different entity phrase receives BOTH a
// flag_alias candidate and a playbook_candidate in the recall
// envelope, follows their two-step next_actions, confirms both, and
// finishes within half of session A's invocation count. Materialization
// is asserted through recall itself: the playbook surfaces in playbook
// proper at confidence 2 and the confirmed flag correction lands in
// the family notes.
func (h *incidentHarness) testAE1IncidentReplayWithConfirm(t *testing.T) {
	home := t.TempDir()
	const sessionA = "incident-session-a"
	const sessionB = "incident-session-b"
	// Harness-only inspection commands run under this key so they never
	// distort the A/B efficiency comparison.
	const harnessKey = "incident-harness-verify"

	// --- Session A: the cold incident. ---
	envA := h.recallEnvelope(home, sessionA, incidentFamilyQueryA)
	require.False(t, envA.Found, "session A recall must miss on a cold store")
	require.Equal(t, incidentExpectedFamily, envA.Normalized, "the fixture query must normalize to the expected family")

	_, _, exit := h.run(home, sessionA, false, "items", "list", "--json")
	require.Equal(t, 0, exit)
	_, _, exit = h.run(home, sessionA, false, "items", "get", "--id", "alpha-recap", "--json")
	require.Equal(t, 0, exit)

	h.runIncident(home, sessionA)

	_, _, exit = h.run(home, sessionA, false, "items", "list", "--select", "winner,score", "--json")
	require.Equal(t, 0, exit)
	_, _, exit = h.run(home, sessionA, false, "items", "get", "--id", "alpha-recap", "--json")
	require.Equal(t, 0, exit)
	_, stderr, exit := h.run(home, sessionA, false, "teach",
		"--query", incidentFamilyQueryA,
		"--resource-type", "items",
		"--resource", "alpha-recap",
		"--json")
	require.Equal(t, 0, exit, "teach must succeed; stderr:\n%s", stderr)

	// Root-cause probe for the synthesis chain: the journal's recall
	// entry must carry the query family. Without it the derivation
	// pass cannot family-anchor the flag correction and teach-time
	// synthesis cannot find its recall->teach episode.
	recallJournaled := false
	for _, e := range h.journalFor(home, sessionA) {
		if len(e.Cmd) > 0 && e.Cmd[0] == "recall" {
			recallJournaled = true
			assert.Equal(t, incidentExpectedFamily, e.QueryFamily,
				"AE1: the recall journal entry must carry the query family (the emitted recall command never stages it via learn.SetJournalLearnContext, so derivation and synthesis have no family anchor)")
		}
	}
	require.True(t, recallJournaled, "session A's recall must be journaled")

	// The store must now hold both quarantined candidates.
	stored := h.listCandidates(home, harnessKey, "open")
	var storedFlag, storedPB *incidentCandidateRow
	for i := range stored {
		switch stored[i].Class {
		case "flag_alias":
			storedFlag = &stored[i]
		case "playbook_candidate":
			storedPB = &stored[i]
		}
	}
	require.NotNil(t, storedFlag, "AE1: the failed/corrected flag pair must derive an open flag_alias candidate")
	assert.Contains(t, string(storedFlag.Payload), `"--select"`, "flag_alias payload must carry the corrected flag")
	assert.Equal(t, incidentExpectedFamily, storedFlag.QueryFamily,
		"AE1: the derived flag_alias must be anchored to the session's recall family (empty means the journal recall entry had no query_family)")
	assert.NotNil(t, storedPB,
		"AE1: session A's teach must synthesize a playbook_candidate from the recall->teach journal episode; none exists in the store")

	// --- Session B: fresh process env, same family, different entity. ---
	envB := h.recallEnvelope(home, sessionB, incidentFamilyQueryB)
	require.Equal(t, incidentExpectedFamily, envB.Normalized, "session B's phrasing must land in the same family")
	assert.Contains(t, envB.Warnings, "candidates_present",
		"AE1: session B's recall envelope must warn candidates_present")

	var envFlag, envPB *incidentEnvelopeCand
	for i := range envB.Candidates {
		switch envB.Candidates[i].Class {
		case "flag_alias":
			envFlag = &envB.Candidates[i]
		case "playbook_candidate":
			envPB = &envB.Candidates[i]
		}
	}
	assert.NotNil(t, envFlag,
		"AE1: the recall envelope must carry the flag_alias candidate (the emitted recallEnvelope drops learn.Result.Candidates, so agents never see the candidates section the candidates_present warning announces)")
	assert.NotNil(t, envPB,
		"AE1: the recall envelope must carry the playbook_candidate")
	for _, c := range []*incidentEnvelopeCand{envFlag, envPB} {
		if c == nil {
			continue
		}
		if assert.Len(t, c.NextAction, 2, "candidate %d next_action must be exactly two steps", c.ID) {
			assert.Equal(t, fmt.Sprintf("%s learnings confirm %d", h.binName, c.ID), c.NextAction[1],
				"candidate %d's second step must be the literal learnings confirm command", c.ID)
		}
	}

	// Candidate IDs for the confirm flow. The envelope is the
	// contractual source; when it lacks the section, session B has to
	// pay for a store listing instead, and that extra invocation is
	// counted against it (the efficiency gate stays honest).
	var flagID, pbID int64
	if envFlag != nil {
		flagID = envFlag.ID
	}
	if envPB != nil {
		pbID = envPB.ID
	}
	if flagID == 0 || pbID == 0 {
		for _, row := range h.listCandidates(home, sessionB, "open") {
			if row.Class == "flag_alias" && flagID == 0 {
				flagID = row.ID
			}
			if row.Class == "playbook_candidate" && pbID == 0 {
				pbID = row.ID
			}
		}
	}

	// Trial step: session B verifies the corrected flag works without
	// ever repeating the failure.
	_, _, exit = h.run(home, sessionB, false, "items", "list", "--select", "winner", "--json")
	require.Equal(t, 0, exit, "session B's trial of the corrected flag must succeed")

	// Confirm both candidates.
	if flagID != 0 {
		stdout, stderr, exit := h.run(home, sessionB, false, "learnings", "confirm", fmt.Sprint(flagID), "--json")
		assert.Equal(t, 0, exit,
			"AE1: confirming the flag_alias candidate must succeed and append the correction to the family notes; stdout:\n%s\nstderr:\n%s", stdout, stderr)
	} else {
		assert.Fail(t, "AE1: no flag_alias candidate id to confirm")
	}
	if pbID != 0 {
		stdout, stderr, exit := h.run(home, sessionB, false, "learnings", "confirm", fmt.Sprint(pbID), "--json")
		assert.Equal(t, 0, exit,
			"AE1: confirming the playbook_candidate must materialize a playbook row at confidence 2; stdout:\n%s\nstderr:\n%s", stdout, stderr)
	} else {
		assert.Fail(t, "AE1: no playbook_candidate id to confirm")
	}

	// Session B must never repeat the failure the loop healed.
	for _, e := range h.journalFor(home, sessionB) {
		assert.Empty(t, e.FailedFlag, "session B must never repeat a flag failure; journaled %v failed with %s", e.Cmd, e.FailedFlag)
		assert.Equal(t, 0, e.ExitCode, "session B invocation %v must not fail", e.Cmd)
	}

	// Materialization is asserted through recall itself, under the
	// harness key so the check does not distort the count.
	envVerify := h.recallEnvelope(home, harnessKey, "why did the Gammas win yesterday")
	if assert.NotNil(t, envVerify.Playbook,
		"AE1: after confirm, recall must return the materialized playbook in playbook proper") {
		assert.Equal(t, 2, envVerify.Playbook.Confidence, "materialized playbook must sit at confidence 2 (the teach floor)")
		assert.NotEmpty(t, envVerify.Playbook.Playbook.Steps, "materialized playbook must carry the synthesized steps")
	}
	assert.Contains(t, envVerify.Notes, "use --select instead of --selec",
		"AE1: the confirmed flag correction must be present in the family notes")

	// The AE1 efficiency gate, with the real numbers.
	countA := h.counts[sessionA]
	countB := h.counts[sessionB]
	assert.LessOrEqual(t, countB*2, countA,
		"AE1: session B took %d invocations (including learnings commands) vs session A's %d; the contract is at most 50 percent", countB, countA)
	t.Logf("AE1 invocation counts: session A = %d, session B = %d", countA, countB)
}

// testAE2NoConfirmSteadyState enforces AE2: nobody ever confirms, and
// candidates still surface session after session with sightings
// intact. Runs in its own state dir so AE1's confirms cannot leak in.
func (h *incidentHarness) testAE2NoConfirmSteadyState(t *testing.T) {
	home := t.TempDir()
	const harnessKey = "steady-harness"

	// Session C1 pays the cost once (recall so the family anchors,
	// then the failure pair, then teach).
	env := h.recallEnvelope(home, "steady-c1", incidentFamilyQueryA)
	require.False(t, env.Found)
	h.runIncident(home, "steady-c1")
	_, _, exit := h.run(home, "steady-c1", false, "teach",
		"--query", incidentFamilyQueryA, "--resource-type", "items", "--resource", "alpha-recap", "--json")
	require.Equal(t, 0, exit)

	// Session C2 (fresh key) hits the identical failure again: the
	// same signature must bump sightings on the one open row, not
	// spawn a second candidate.
	h.runIncident(home, "steady-c2")

	open := h.listCandidates(home, harnessKey, "open")
	var flagRows []incidentCandidateRow
	for _, row := range open {
		if row.Class == "flag_alias" {
			flagRows = append(flagRows, row)
		}
	}
	require.Len(t, flagRows, 1, "the identical correction must converge on one open flag_alias candidate")
	require.Equal(t, 2, flagRows[0].Sightings, "the repeat observation must bump sightings to 2")

	// Session C3 recalls the family with a different entity and no
	// confirm has ever happened: the envelope must still carry the
	// high-sighting candidate with sightings intact.
	envC3 := h.recallEnvelope(home, "steady-c3", incidentFamilyQueryB)
	assert.Contains(t, envC3.Warnings, "candidates_present",
		"AE2: unconfirmed candidates must keep surfacing in later sessions")
	var surfaced *incidentEnvelopeCand
	for i := range envC3.Candidates {
		if envC3.Candidates[i].Class == "flag_alias" {
			surfaced = &envC3.Candidates[i]
		}
	}
	if assert.NotNil(t, surfaced,
		"AE2: session C's recall envelope must carry the unconfirmed flag_alias candidate (envelope drops learn.Result.Candidates today)") {
		assert.Equal(t, 2, surfaced.Sightings, "AE2: surfaced candidate must keep its accumulated sightings")
	}

	// Steady state means still no confirmation: the row stays open.
	openAfter := h.listCandidates(home, harnessKey, "open")
	require.NotEmpty(t, openAfter, "no-confirm steady state must leave the candidate open")
}

// testAE3RejectRecovery enforces AE3: a rejected candidate tombstones
// its derivation signature, the identical failure pair re-derives
// nothing, and recall never resurfaces it.
func (h *incidentHarness) testAE3RejectRecovery(t *testing.T) {
	home := t.TempDir()
	const harnessKey = "reject-harness"

	h.runIncident(home, "reject-r1")
	open := h.listCandidates(home, harnessKey, "open")
	require.Len(t, open, 1, "the failure pair must derive exactly one candidate")
	require.Equal(t, "flag_alias", open[0].Class)

	stdout, stderr, exit := h.run(home, harnessKey, false, "learnings", "reject", fmt.Sprint(open[0].ID), "--json")
	require.Equal(t, 0, exit, "reject must succeed; stdout:\n%s\nstderr:\n%s", stdout, stderr)
	require.Contains(t, stdout, `"rejected": true`)

	// Replay the identical failure pair in a fresh session.
	h.runIncident(home, "reject-r2")

	require.Empty(t, h.listCandidates(home, harnessKey, "open"),
		"AE3: the tombstoned signature must not re-derive an open candidate")
	all := h.listCandidates(home, harnessKey, "")
	require.Len(t, all, 1, "AE3: the replay must not create a second candidate row")
	require.Equal(t, "rejected", all[0].Status, "the tombstone row must stay rejected")
	require.Equal(t, 1, all[0].Sightings, "AE3: the replayed observation must be dropped, not counted onto the tombstone")

	env := h.recallEnvelope(home, "reject-r3", incidentFamilyQueryA)
	assert.NotContains(t, env.Warnings, "candidates_present",
		"AE3: recall must never resurface a rejected candidate")
	assert.Empty(t, env.Candidates, "AE3: the rejected candidate must not ride the envelope")
}

// testAE4PhrasingConvergence enforces AE4: a playbook taught under the
// "yesterday" phrasing resolves for a recall phrased "last night" (the
// default same-referent synonym fold), same family, same playbook.
func (h *incidentHarness) testAE4PhrasingConvergence(t *testing.T) {
	home := t.TempDir()

	_, stderr, exit := h.run(home, "phrase-teach", false, "teach",
		"--query", incidentFamilyQueryA,
		"--resource-type", "items",
		"--resource", "alpha-recap",
		"--playbook-json", `{"steps":[{"cmd":"items list --select winner"}],"expected_tool_calls":1}`,
		"--json")
	require.Equal(t, 0, exit, "teach with an inline playbook must succeed; stderr:\n%s", stderr)

	env := h.recallEnvelope(home, "phrase-recall", "why did the Bravos win last night")
	require.Equal(t, incidentExpectedFamily, env.Normalized,
		"AE4: the last-night phrasing must fold into the yesterday family")
	require.NotNil(t, env.Playbook, "AE4: the same playbook must surface under the folded phrasing")
	require.Equal(t, incidentExpectedFamily, env.Playbook.QueryFamily)
	require.Equal(t, 2, env.Playbook.Confidence)
	require.Len(t, env.Playbook.Playbook.Steps, 1)
	require.Equal(t, "items list --select winner", env.Playbook.Playbook.Steps[0].Cmd)
}

// testAE6HarnessSilence enforces AE6: commands run under
// PRINTING_PRESS_VERIFY=1 leave zero journal entries, zero candidates,
// and zero learn_events rows.
func (h *incidentHarness) testAE6HarnessSilence(t *testing.T) {
	home := t.TempDir()
	const harnessKey = "verify-harness"

	// The full incident, all under the verify env.
	_, _, exit := h.run(home, "verify-s", true, "recall", incidentFamilyQueryA, "--json")
	require.Equal(t, 0, exit)
	_, _, exit = h.run(home, "verify-s", true, "items", "list", "--selec", "winner", "--json")
	require.Equal(t, 2, exit)
	_, _, exit = h.run(home, "verify-s", true, "items", "list", "--select", "winner", "--json")
	require.Equal(t, 0, exit)
	_, _, exit = h.run(home, "verify-s", true, "teach",
		"--query", incidentFamilyQueryA, "--resource-type", "items", "--resource", "alpha-recap", "--json")
	require.Equal(t, 0, exit)

	// Zero journal entries: the learn/ state subdir must not even exist.
	segments, err := filepath.Glob(filepath.Join(home, "state", "learn", "journal-*.jsonl"))
	require.NoError(t, err)
	assert.Empty(t, segments, "AE6: verify-env runs must write no journal segments")

	// Zero candidates. The inspection runs without the verify env so
	// it can actually read the store.
	assert.Empty(t, h.listCandidates(home, harnessKey, ""),
		"AE6: verify-env runs must derive no candidates")

	// Zero learn_events rows, via learnings stats.
	stdout, stderr, exit := h.run(home, harnessKey, false, "learnings", "stats", "--json")
	require.Equal(t, 0, exit, "learnings stats must exit 0; stderr:\n%s", stderr)
	var stats struct {
		RecallHits   int            `json:"recall_hits"`
		RecallMisses int            `json:"recall_misses"`
		EventCounts  map[string]int `json:"event_counts"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &stats), "stats must be JSON; got:\n%s", stdout)
	assert.Empty(t, stats.EventCounts,
		"AE6: verify-env runs must record zero learn_events rows (recall and teach still insert recall_miss/teach events under PRINTING_PRESS_VERIFY=1)")
	assert.Zero(t, stats.RecallMisses, "AE6: the verify-env recall must not record a recall_miss event")
	assert.Zero(t, stats.RecallHits, "AE6: the verify-env recall must not record a recall_hit event")
}

// testSessionKeyFallbackPidLineage validates KTD11's pid-lineage
// fallback: with NO <PREFIX>_LEARN_SESSION in the environment, the
// journal keys entries by parent pid. Both invocations here are
// spawned directly by this test process, one parent for both, which is
// the contract's model of an agent shell issuing sequential commands:
// they must share a ppid:<pid> key and the failure pair must still
// derive a candidate.
//
// The honest boundary, verified while building this harness: the
// fallback pairs ONLY when invocations share the literal parent
// process. A harness that wraps each invocation in its own short-lived
// shell (a fresh `sh -c` per command, the way some agent runtimes
// exec) gives every invocation a different ppid, the keys diverge, and
// derivation correctly refuses to pair across them. Same-parent
// spawning is therefore the contract this subtest pins.
func (h *incidentHarness) testSessionKeyFallbackPidLineage(t *testing.T) {
	home := t.TempDir()

	// sessionKey "" omits the env var entirely.
	h.runIncident(home, "")

	entries := h.readJournal(home)
	require.Len(t, entries, 2, "both invocations must journal")
	require.True(t, strings.HasPrefix(entries[0].SessionKey, "ppid:"),
		"with no session env var the key must fall back to pid lineage; got %q", entries[0].SessionKey)
	require.Equal(t, entries[0].SessionKey, entries[1].SessionKey,
		"invocations spawned by one parent must share a session key")

	open := h.listCandidates(home, "pid-harness", "open")
	require.Len(t, open, 1, "pid-lineage keyed failure pair must still derive the flag_alias candidate")
	require.Equal(t, "flag_alias", open[0].Class)
}
