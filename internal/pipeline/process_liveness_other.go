//go:build !unix && !windows

package pipeline

func lockOwnerAlive(pid int) bool {
	return pid > 0
}
