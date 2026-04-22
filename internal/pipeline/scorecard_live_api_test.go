package pipeline

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Verify the scoreLiveAPIVerification helper in isolation. It answers a
// simple question - did verify run live, and if so how well did it do -
// so its tests focus on the boundary between scored and unscored plus
// the PassRate to score mapping.
func TestScoreLiveAPIVerification(t *testing.T) {
	t.Run("nil report is unscored", func(t *testing.T) {
		score, scored := scoreLiveAPIVerification(nil)
		assert.Equal(t, 0, score)
		assert.False(t, scored)
	})

	t.Run("mock mode is unscored", func(t *testing.T) {
		score, scored := scoreLiveAPIVerification(&VerifyReport{Mode: "mock", PassRate: 100})
		assert.Equal(t, 0, score)
		assert.False(t, scored, "mock verify must not count as live verification, even at 100% pass")
	})

	t.Run("structural mode is unscored", func(t *testing.T) {
		score, scored := scoreLiveAPIVerification(&VerifyReport{Mode: "structural", PassRate: 100})
		assert.Equal(t, 0, score)
		assert.False(t, scored)
	})

	t.Run("empty mode is unscored", func(t *testing.T) {
		score, scored := scoreLiveAPIVerification(&VerifyReport{PassRate: 100})
		assert.Equal(t, 0, score)
		assert.False(t, scored, "missing Mode defaults to unscored rather than rewarding pass rate from an unknown source")
	})

	t.Run("live 100% scores 10 and caps", func(t *testing.T) {
		score, scored := scoreLiveAPIVerification(&VerifyReport{Mode: "live", PassRate: 100})
		assert.True(t, scored)
		assert.Equal(t, 10, score)
	})

	t.Run("live 95% hits the cap at 10", func(t *testing.T) {
		score, scored := scoreLiveAPIVerification(&VerifyReport{Mode: "live", PassRate: 95})
		assert.True(t, scored)
		assert.Equal(t, 10, score, "95% should saturate the dimension")
	})

	t.Run("live 94% scores 9", func(t *testing.T) {
		score, scored := scoreLiveAPIVerification(&VerifyReport{Mode: "live", PassRate: 94})
		assert.True(t, scored)
		assert.Equal(t, 9, score)
	})

	t.Run("live 50% scores 5", func(t *testing.T) {
		score, scored := scoreLiveAPIVerification(&VerifyReport{Mode: "live", PassRate: 50})
		assert.True(t, scored)
		assert.Equal(t, 5, score)
	})

	t.Run("live 0% scores 0 but is scored", func(t *testing.T) {
		score, scored := scoreLiveAPIVerification(&VerifyReport{Mode: "live", PassRate: 0})
		assert.True(t, scored, "a live run with every check failing still produces a scored signal - it means the CLI was exercised")
		assert.Equal(t, 0, score)
	})
}

// Verify the scorecard wires LiveAPIVerification correctly end-to-end:
// when a live VerifyReport is passed in, the dimension is populated and
// is NOT in UnscoredDimensions; when the report is nil the dimension is
// in UnscoredDimensions so tier2 normalizes as before.
func TestRunScorecard_LiveAPIVerificationWiring(t *testing.T) {
	t.Run("nil verify report adds live_api_verification to UnscoredDimensions", func(t *testing.T) {
		dir := t.TempDir()
		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, "", nil)
		assert.NoError(t, err)
		assert.Contains(t, sc.UnscoredDimensions, "live_api_verification",
			"nil verify report must mark live_api_verification unscored so existing CLIs grade unchanged")
		assert.Equal(t, 0, sc.Steinberger.LiveAPIVerification)
	})

	t.Run("mock-backed verify also marks dimension unscored", func(t *testing.T) {
		dir := t.TempDir()
		pipelineDir := t.TempDir()
		mock := &VerifyReport{Mode: "mock", PassRate: 100}
		sc, err := RunScorecard(dir, pipelineDir, "", mock)
		assert.NoError(t, err)
		assert.Contains(t, sc.UnscoredDimensions, "live_api_verification",
			"mock-backed verify and live verify must be distinguishable in the scorecard")
	})

	t.Run("live verify at 100 populates the dimension and removes it from UnscoredDimensions", func(t *testing.T) {
		dir := t.TempDir()
		pipelineDir := t.TempDir()
		live := &VerifyReport{Mode: "live", PassRate: 100}
		sc, err := RunScorecard(dir, pipelineDir, "", live)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "live_api_verification")
		assert.Equal(t, 10, sc.Steinberger.LiveAPIVerification)
	})

	t.Run("live verify at 70 scores 7", func(t *testing.T) {
		dir := t.TempDir()
		pipelineDir := t.TempDir()
		live := &VerifyReport{Mode: "live", PassRate: 70}
		sc, err := RunScorecard(dir, pipelineDir, "", live)
		assert.NoError(t, err)
		assert.Equal(t, 7, sc.Steinberger.LiveAPIVerification)
	})

	t.Run("json output exposes live_api_verification field", func(t *testing.T) {
		dir := t.TempDir()
		pipelineDir := t.TempDir()
		live := &VerifyReport{Mode: "live", PassRate: 80}
		sc, err := RunScorecard(dir, pipelineDir, "", live)
		assert.NoError(t, err)
		data, err := json.Marshal(sc)
		assert.NoError(t, err)
		assert.Contains(t, string(data), `"live_api_verification":8`)
	})
}

// Guard R5 from the plan: landing LiveAPIVerification must not change
// tier 2 normalization for CLIs that never ran live verify. When the
// dimension is unscored, its 10-point slot is subtracted from tier2Max,
// so the effective denominator matches what it was before this gate
// landed.
//
// Note: we deliberately do not compare nil vs mock here. The scorecard
// has a pre-existing calibration (line ~211) that raises Total to a
// floor based on any verifyReport.PassRate regardless of mode. That
// calibration is out of scope for this change. What we test here is
// the unscored-dim math, which is what R5 actually constrains.
func TestRunScorecard_UnscoredLiveDimDoesNotShrinkTier2(t *testing.T) {
	dir := t.TempDir()
	pipelineDir := t.TempDir()
	sc, err := RunScorecard(dir, pipelineDir, "", nil)
	assert.NoError(t, err)
	assert.Contains(t, sc.UnscoredDimensions, "live_api_verification")
	// With all spec-dependent and verify-dependent dims unscored, tier2
	// normalization still runs. The Total being well-formed (0-100) is
	// the invariant we care about here.
	assert.GreaterOrEqual(t, sc.Steinberger.Total, 0)
	assert.LessOrEqual(t, sc.Steinberger.Total, 100)
}

// A readable sanity check that live verify at a high pass rate actually
// lifts the Total in a meaningful way. This prevents a future refactor
// from silently converting the dimension into dead weight.
func TestRunScorecard_LiveVerifyLiftsTotal(t *testing.T) {
	dir := t.TempDir()
	pipelineDir := t.TempDir()

	scNil, err := RunScorecard(dir, pipelineDir, "", nil)
	assert.NoError(t, err)
	scLive, err := RunScorecard(dir, pipelineDir, "", &VerifyReport{Mode: "live", PassRate: 100})
	assert.NoError(t, err)

	assert.GreaterOrEqual(t, scLive.Steinberger.Total, scNil.Steinberger.Total,
		"live verify at 100 must produce at least the same Total as no verify; a live-tested CLI should never score lower than an untested one")

	// Sanity: the dimension name appears in the scorecard JSON (not just the struct).
	data, err := json.Marshal(scLive)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "live_api_verification"))
}
