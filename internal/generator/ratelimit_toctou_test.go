package generator

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAdaptiveLimiterWait_ReservesUnderSingleLock validates the generated
// cliutil AdaptiveLimiter implementation reserves request slots while holding
// one contiguous lock span in Wait().
func TestAdaptiveLimiterWait_ReservesUnderSingleLock(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("ratelimit-toctou")
	outputDir := filepath.Join(t.TempDir(), "ratelimit-toctou-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	srcBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "ratelimit.go"))
	require.NoError(t, err)
	src := string(srcBytes)

	require.Contains(t, src, "l.lastRequest = time.Now().Add(sleep)",
		"Wait must reserve the next slot under lock")

	waitStart := strings.Index(src, "func (l *AdaptiveLimiter) Wait() {")
	require.NotEqual(t, -1, waitStart, "Wait function must be emitted")
	onSuccessStart := strings.Index(src[waitStart:], "func (l *AdaptiveLimiter) OnSuccess()")
	require.NotEqual(t, -1, onSuccessStart, "OnSuccess marker must be emitted after Wait")
	waitBody := src[waitStart : waitStart+onSuccessStart]

	require.Equal(t, 1, strings.Count(waitBody, "l.mu.Lock()"),
		"Wait should lock once and keep the lock across read+reservation")
	require.Equal(t, 1, strings.Count(waitBody, "l.mu.Unlock()"),
		"Wait should unlock once after reserving lastRequest")

	writeIdx := strings.Index(waitBody, "l.lastRequest = time.Now().Add(sleep)")
	lockIdx := strings.Index(waitBody, "l.mu.Lock()")
	unlockIdx := strings.Index(waitBody, "l.mu.Unlock()")
	require.NotEqual(t, -1, writeIdx, "reservation write must exist in Wait")
	require.NotEqual(t, -1, lockIdx, "lock call must exist in Wait")
	require.NotEqual(t, -1, unlockIdx, "unlock call must exist in Wait")
	require.Less(t, lockIdx, writeIdx, "Wait must hold lock before reservation write")
	require.Less(t, writeIdx, unlockIdx, "Wait must not unlock before reservation write")
}

func TestAdaptiveLimiterFloor_AllowsBackoffToHalfRPS(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("ratelimit-floor")
	outputDir := filepath.Join(t.TempDir(), "ratelimit-floor-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	srcBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "ratelimit.go"))
	require.NoError(t, err)
	src := string(srcBytes)

	require.Contains(t, src, "floor := 0.5")
	require.Contains(t, src, "if ratePerSec < floor {")
	require.Contains(t, src, "floor:     floor,")
	require.Contains(t, src, "if l.rate < l.floor {")
	require.Contains(t, src, "if newRate < l.floor {")
	require.NotContains(t, src, "floor:     ratePerSec,")

	requireGeneratedTestsPass(
		t,
		outputDir,
		"TestAdaptiveLimiter_(HalvesOnRateLimit|FloorsAtHalfRPS|DoesNotRaiseSubFloorRateOnRateLimit|DoesNotRampBelowFloorAfterRateLimit)$",
		[]string{
			"TestAdaptiveLimiter_HalvesOnRateLimit",
			"TestAdaptiveLimiter_FloorsAtHalfRPS",
			"TestAdaptiveLimiter_DoesNotRaiseSubFloorRateOnRateLimit",
			"TestAdaptiveLimiter_DoesNotRampBelowFloorAfterRateLimit",
		},
	)
}

func requireGeneratedTestsPass(t *testing.T, dir, pattern string, want []string) {
	t.Helper()

	cmd := exec.Command("go", "test", "-mod=mod", "-json", "./internal/cliutil", "-run", pattern)
	cmd.Dir = dir
	cacheDir, err := goBuildCacheDir(dir)
	require.NoError(t, err)
	cmd.Env = append(os.Environ(), "GOCACHE="+cacheDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	require.NoError(t, err, stderr.String())

	var event struct {
		Action string `json:"Action"`
		Test   string `json:"Test"`
	}
	seen := map[string]bool{}
	dec := json.NewDecoder(&stdout)
	for dec.Decode(&event) == nil {
		if event.Action == "pass" && event.Test != "" {
			seen[event.Test] = true
		}
	}
	for _, name := range want {
		require.Truef(t, seen[name], "generated test selector %q did not run %s", pattern, name)
	}
}
