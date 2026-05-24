package generator

import (
	"os"
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
