//go:build !darwin

package pressauth

import "errors"

// errKeyNotFound mirrors the darwin sentinel so Save's getOrCreateKey can
// pattern-match identically on either platform — even though every
// keychain call on non-darwin returns errKeychainUnsupported below.
var errKeyNotFound = errors.New("no keychain entry")

// errKeychainUnsupported is the consolidated message every keychain entry
// point returns on Linux and Windows. The message is intentionally
// user-shaped: it shows up directly when somebody runs press-auth on an
// unsupported OS.
var errKeychainUnsupported = errors.New("press-auth currently requires macOS — Linux/Windows support is planned, see plan")

func loadKey(_ string) ([]byte, error) { return nil, errKeychainUnsupported }
func saveKey(_ string, _ []byte) error { return errKeychainUnsupported }
func deleteKey(_ string) error         { return errKeychainUnsupported }

func isKeychainUnsupported(err error) bool { return errors.Is(err, errKeychainUnsupported) }
