package generator

import "sync"

// stderrSwapMu serializes tests that temporarily replace the process-global
// os.Stderr (and os.Stdout) with a pipe. Two such tests overlapping restore
// each other's stream mid-capture, sending expected output to the wrong
// sink; every swap site holds this lock for the full swap-write-restore
// window.
var stderrSwapMu sync.Mutex
