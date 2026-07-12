//go:build darwin

package pressauth

import (
	"errors"
	"fmt"

	keychain "github.com/keybase/go-keychain"
)

// keychainAccount is the fixed account name press-auth stores under. We
// vary the Service instead (one per domain) so the user can audit and
// revoke per-domain access from Keychain Access.app.
const keychainAccount = "press-auth"

// keychainServicePrefix prefixes the per-domain Service entry so all
// press-auth keychain items sort together when the user browses them.
const keychainServicePrefix = "press-auth: "

// errKeyNotFound signals "no keychain entry for this domain" and is the
// trigger Save uses to generate a fresh key. Distinct from real keychain
// errors so callers can pattern-match on it.
var errKeyNotFound = errors.New("no keychain entry")

func keychainService(domain string) string {
	return keychainServicePrefix + domain
}

// loadKey fetches the 32-byte AES key for domain. A miss surfaces as
// errKeyNotFound; any other failure (locked keychain, denied permission,
// IO error) returns a wrapped error.
func loadKey(domain string) ([]byte, error) {
	data, err := keychain.GetGenericPassword(keychainService(domain), keychainAccount, "", "")
	if err != nil {
		return nil, fmt.Errorf("reading keychain: %w", err)
	}
	if data == nil {
		return nil, errKeyNotFound
	}
	return data, nil
}

// saveKey stores key in the macOS keychain under the per-domain Service.
// On macOS the user sees an "Always Allow / Allow / Deny" prompt the first
// time per service; subsequent reads from the same binary are silent if
// they pick Always Allow.
func saveKey(domain string, key []byte) error {
	item := keychain.NewGenericPassword(
		keychainService(domain),
		keychainAccount,
		"",
		key,
		"",
	)
	item.SetSynchronizable(keychain.SynchronizableNo)
	item.SetAccessible(keychain.AccessibleWhenUnlocked)

	err := keychain.AddItem(item)
	if err == nil {
		return nil
	}
	if !errors.Is(err, keychain.ErrorDuplicateItem) {
		return fmt.Errorf("adding keychain item: %w", err)
	}

	// Duplicate — overwrite via UpdateItem so a rotated key replaces the
	// old one in place rather than colliding on insert.
	query := keychain.NewItem()
	query.SetSecClass(keychain.SecClassGenericPassword)
	query.SetService(keychainService(domain))
	query.SetAccount(keychainAccount)

	update := keychain.NewItem()
	update.SetData(key)
	if err := keychain.UpdateItem(query, update); err != nil {
		return fmt.Errorf("updating keychain item: %w", err)
	}
	return nil
}

// deleteKey removes the keychain entry for domain. A miss is idempotent
// (no error returned) so callers can use it as the body of `forget`.
func deleteKey(domain string) error {
	err := keychain.DeleteGenericPasswordItem(keychainService(domain), keychainAccount)
	if err == nil {
		return nil
	}
	if errors.Is(err, keychain.ErrorItemNotFound) {
		return nil
	}
	return fmt.Errorf("deleting keychain item: %w", err)
}

func isKeychainUnsupported(error) bool { return false }
