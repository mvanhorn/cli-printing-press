package cli

import (
	"errors"
	"fmt"
)

// Exit codes for structured error reporting.
const (
	ExitSuccess         = 0
	ExitInputError      = 1
	ExitSpecError       = 2
	ExitGenerationError = 3
	ExitUnknownError    = 4
	ExitPublishError    = 5
)

// ExitError wraps an error with a specific exit code.
// When Silent is true, main should exit with the code but not print the
// error message — used when structured output (--json) already contains
// the failure details.
type ExitError struct {
	Code   int
	Err    error
	Silent bool
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// Type assertion misses fmt.Errorf wraps from restore-after-cancel.
func asExitError(err error) *ExitError {
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr
	}
	return nil
}

func wrapKeepingExitClass(err, extra error) error {
	if extra == nil {
		return err
	}
	wrapped := fmt.Errorf("%w; %v", err, extra)
	if exitErr := asExitError(err); exitErr != nil {
		return &ExitError{Code: exitErr.Code, Silent: exitErr.Silent, Err: wrapped}
	}
	return wrapped
}
