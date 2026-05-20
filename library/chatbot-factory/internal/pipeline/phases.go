// internal/pipeline/phases.go
package pipeline

// PhaseOrder lists pipeline phases in execution order.
var PhaseOrder = []string{
	"preflight",
	"init",
	"chunk",
	"enrich",
	"db-setup",
	"upload-rag",
	"scaffold",
	"style-apply",
	"customize",
	"audit",
	"env-setup",
	"test-local",
	"deploy-bootstrap",
	"deploy-up",
	"test-sandbox",
	"channel-config",
	"ship",
}

// PhaseNumber maps phase name to numeric id (used in plan-file prefixes).
var PhaseNumber = map[string]int{
	"preflight":        0,
	"init":             10,
	"chunk":            20,
	"enrich":           30,
	"db-setup":         40,
	"upload-rag":       50,
	"scaffold":         60,
	"style-apply":      70,
	"customize":        80,
	"audit":            90,
	"env-setup":        100,
	"test-local":       110,
	"deploy-bootstrap": 120,
	"deploy-up":        130,
	"test-sandbox":     140,
	"channel-config":   150,
	"ship":             160,
}

// IsPhase returns true if name is a known phase.
func IsPhase(name string) bool {
	_, ok := PhaseNumber[name]
	return ok
}
