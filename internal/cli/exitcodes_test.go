package cli

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitError_Error(t *testing.T) {
	err := &ExitError{Code: ExitSpecError, Err: fmt.Errorf("spec not found")}
	if err.Error() != "spec not found" {
		t.Errorf("got %q, want %q", err.Error(), "spec not found")
	}
}

func TestExitError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("inner error")
	err := &ExitError{Code: ExitInputError, Err: fmt.Errorf("wrapping: %w", inner)}
	if !errors.Is(err, inner) {
		t.Error("errors.Is should find inner error through ExitError")
	}
}

func TestExitError_As(t *testing.T) {
	err := &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("build failed")}
	wrapped := fmt.Errorf("command failed: %w", err)

	var exitErr *ExitError
	if !errors.As(wrapped, &exitErr) {
		t.Fatal("errors.As should extract ExitError from wrapped error")
	}
	if exitErr.Code != ExitGenerationError {
		t.Errorf("got code %d, want %d", exitErr.Code, ExitGenerationError)
	}
}

func TestAsExitErrorUnwrapsWrappedCancellation(t *testing.T) {
	inner := &ExitError{Code: ExitInputError, Err: fmt.Errorf("cancelled — non-generator-owned files left intact")}
	wrapped := fmt.Errorf("%w; restoring tree after refused hand-authored delete: boom", inner)

	if _, ok := wrapped.(*ExitError); ok {
		t.Fatal("direct type assertion must not see the wrapped ExitError")
	}
	got := asExitError(wrapped)
	if got == nil {
		t.Fatal("asExitError should unwrap the input-class ExitError")
	}
	if got.Code != ExitInputError {
		t.Errorf("got code %d, want %d", got.Code, ExitInputError)
	}
}

func TestWrapKeepingExitClassPreservesInputCode(t *testing.T) {
	cancel := &ExitError{Code: ExitInputError, Err: fmt.Errorf("cancelled — non-generator-owned files left intact")}
	restore := fmt.Errorf("restoring tree after refused hand-authored delete: boom")

	got := wrapKeepingExitClass(cancel, restore)
	exitErr := asExitError(got)
	if exitErr == nil {
		t.Fatal("wrapKeepingExitClass should keep an ExitError")
	}
	if exitErr.Code != ExitInputError {
		t.Errorf("got code %d, want %d", exitErr.Code, ExitInputError)
	}
	if !errors.Is(got, cancel) {
		t.Error("wrapped error should still unwrap to the cancellation")
	}
	if got.Error() == cancel.Error() {
		t.Error("wrapped error should include the restore failure")
	}
}

func TestExitCodes_Values(t *testing.T) {
	if ExitSuccess != 0 {
		t.Errorf("ExitSuccess = %d, want 0", ExitSuccess)
	}
	if ExitInputError != 1 {
		t.Errorf("ExitInputError = %d, want 1", ExitInputError)
	}
	if ExitSpecError != 2 {
		t.Errorf("ExitSpecError = %d, want 2", ExitSpecError)
	}
	if ExitGenerationError != 3 {
		t.Errorf("ExitGenerationError = %d, want 3", ExitGenerationError)
	}
	if ExitUnknownError != 4 {
		t.Errorf("ExitUnknownError = %d, want 4", ExitUnknownError)
	}
}
