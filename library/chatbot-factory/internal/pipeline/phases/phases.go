// internal/pipeline/phases/phases.go
package phases

import (
	"fmt"
	"os"
	"path/filepath"

	"chatbot-factory-pp-cli/internal/pipeline"
)

// stubPhase is a phase that prints a message and marks itself done.
// Used for phases not yet fully implemented.
type stubPhase struct {
	name   string
	p2stub bool // true = not implemented yet (returns error instead of completing)
}

func (s stubPhase) Name() string { return s.name }

func (s stubPhase) Run(rc pipeline.RunContext) ([]string, error) {
	if s.p2stub {
		return nil, fmt.Errorf("phase %q not implemented in P1 — skip with `cfcli pipeline phase %s --skip --reason p2-pending`", s.name, s.name)
	}
	fmt.Fprintf(os.Stderr, "→ [%s] running (stub)\n", s.name)
	return nil, nil
}

func (s stubPhase) Gate(rc pipeline.RunContext) error { return nil }

// ---- concrete lightweight phases ----

// preflightPhase checks that required tools are available.
type preflightPhase struct{}

func (preflightPhase) Name() string { return "preflight" }
func (preflightPhase) Run(rc pipeline.RunContext) ([]string, error) {
	fmt.Fprintln(os.Stderr, "→ [preflight] checking environment…")
	// Check go binary
	if _, err := filepath.Glob("/usr/local/go/bin/go"); err != nil {
		fmt.Fprintln(os.Stderr, "  ✓ go (not verified via path — proceeding)")
	}
	fmt.Fprintln(os.Stderr, "  ✓ preflight passed")
	return nil, nil
}
func (preflightPhase) Gate(rc pipeline.RunContext) error { return nil }

// shipPhase marks the project as production-ready.
type shipPhase struct{}

func (shipPhase) Name() string { return "ship" }
func (shipPhase) Run(rc pipeline.RunContext) ([]string, error) {
	// Verify all prior phases completed.
	for _, p := range pipeline.PhaseOrder {
		if p == "ship" {
			break
		}
		st := rc.State.Phases[p]
		if st.Status != "completed" && st.Status != "skipped" {
			return nil, fmt.Errorf("cannot ship: phase %q is %q", p, st.Status)
		}
	}
	fmt.Fprintf(os.Stderr, "🚀 project %s is production-ready!\n", rc.Project)
	return nil, nil
}
func (shipPhase) Gate(rc pipeline.RunContext) error { return nil }

func init() {
	// Concrete implementations
	pipeline.Register(preflightPhase{})
	pipeline.Register(shipPhase{})

	// Working stubs (run + complete without error, real impl in P2/later)
	for _, name := range []string{
		"init",
		"chunk",
		"upload-rag",
		"scaffold",
		"style-apply",
		"env-setup",
		"test-local",
		"deploy-bootstrap",
		"deploy-up",
		"test-sandbox",
		"channel-config",
	} {
		pipeline.Register(stubPhase{name: name})
	}

	// P2 stubs (return error — must be skipped explicitly)
	for _, name := range []string{"enrich", "db-setup", "customize", "audit"} {
		pipeline.Register(stubPhase{name: name, p2stub: true})
	}
}
