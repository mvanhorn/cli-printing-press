//go:build !windows

package pipeline

import (
	"errors"
	"syscall"
)

func isTextFileBusy(err error) bool {
	return errors.Is(err, syscall.ETXTBSY)
}
