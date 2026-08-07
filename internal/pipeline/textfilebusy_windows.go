//go:build windows

package pipeline

func isTextFileBusy(error) bool {
	return false
}
