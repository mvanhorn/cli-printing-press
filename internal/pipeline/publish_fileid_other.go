//go:build !unix && !windows

package pipeline

import "io/fs"

func fileIdentityOf(string, fs.FileInfo) (fileIdentity, bool) {
	return fileIdentity{}, false
}
