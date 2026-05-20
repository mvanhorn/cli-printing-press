package pipeline

import "context"

// RunContext carries everything a phase needs at runtime.
type RunContext struct {
	Ctx        context.Context
	Project    string
	ProjectDir string
	State      *State
	JSONOutput bool
	StatePath  string
}

// Phase is the contract every pipeline phase satisfies.
type Phase interface {
	Name() string
	Run(rc RunContext) (artifacts []string, err error)
	Gate(rc RunContext) error
}
