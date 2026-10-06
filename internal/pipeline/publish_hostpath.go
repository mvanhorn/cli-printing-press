package pipeline

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
)

// Operator home and .runstate paths still appear in notes after live
// transcripts are omitted. A home prefix is rewritten only when the path
// also contains printing-press or a dotfile component, so a route such as
// /home/timeline stays. An absolute path with a .runstate component is
// rewritten through that component.
func redactAbsoluteHostPathsInTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !shouldScanHostPaths(data) {
			return nil
		}
		redacted := redactAbsoluteHostPaths(string(data))
		if redacted == string(data) {
			return nil
		}
		return writeRedactedFile(path, []byte(redacted), info.Mode().Perm())
	})
}

// Copy keeps the source mode. A later open of a 0444 file is not writable
// even for the owner, so packaging would fail on the first read-only note.
func writeRedactedFile(path string, data []byte, perm os.FileMode) error {
	if perm&0o200 == 0 {
		if err := os.Chmod(path, perm|0o200); err != nil {
			return err
		}
	}
	err := os.WriteFile(path, data, perm)
	if perm&0o200 == 0 {
		if chmodErr := os.Chmod(path, perm); chmodErr != nil && err == nil {
			return chmodErr
		}
	}
	return err
}

func shouldScanHostPaths(data []byte) bool {
	if bytes.Contains(data, []byte{0}) || !utf8.Valid(data) {
		return false
	}
	return bytes.Contains(data, []byte("/Users/")) ||
		bytes.Contains(data, []byte("/home/")) ||
		bytes.Contains(data, []byte(`\Users\`)) ||
		bytes.Contains(data, []byte(`\\Users\\`)) ||
		bytes.Contains(data, []byte(".runstate"))
}

func redactAbsoluteHostPaths(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		n := absoluteHostPathLen(s, i)
		if n > 0 {
			b.WriteString(redactOneAbsolutePath(s[i : i+n]))
			i += n
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func absoluteHostPathLen(s string, i int) int {
	if i > 0 && isHostPathByte(s[i-1]) {
		return 0
	}
	end := i
	for end < len(s) && isHostPathByte(s[end]) {
		end++
	}
	if end == i || !isHomeOrRunstateToken(s[i:end]) {
		return 0
	}
	return end - i
}

func isHostPathByte(b byte) bool {
	switch b {
	case '/', '\\', ':', '.', '_', '-', '~', '%', '+', '@':
		return true
	default:
		return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
	}
}

func isHomeOrRunstateToken(token string) bool {
	if !isAbsolutePathToken(token) {
		return false
	}
	if indexAfterRunstate(token) >= 0 {
		return true
	}
	if homePrefixLen(token) == 0 {
		return false
	}
	return hasPathComponent(token, "printing-press") || hasDotfileComponent(token)
}

func isAbsolutePathToken(token string) bool {
	if strings.HasPrefix(token, "/") {
		return true
	}
	return len(token) >= 3 && isDriveLetter(token[0]) && token[1] == ':' && (token[2] == '/' || token[2] == '\\')
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func redactOneAbsolutePath(token string) string {
	if end := indexAfterRunstate(token); end >= 0 {
		return joinPlaceholder(artifacts.RunStatePlaceholder, token, end)
	}
	if prefix := homePrefixLen(token); prefix > 0 {
		return joinPlaceholder(artifacts.CLIDirPlaceholder, token, prefix)
	}
	return token
}

func joinPlaceholder(placeholder, token string, restAt int) string {
	if restAt >= len(token) {
		return placeholder
	}
	rest := strings.TrimLeft(token[restAt:], `/\`)
	if rest == "" {
		return placeholder
	}
	return placeholder + pathSepAt(token, restAt) + rest
}

func pathSepAt(token string, i int) string {
	if strings.HasPrefix(token[i:], `\\`) {
		return `\\`
	}
	if token[i] == '\\' || token[i] == '/' {
		return string(token[i])
	}
	return "/"
}

func indexAfterRunstate(token string) int {
	const name = ".runstate"
	for from := 0; from < len(token); {
		rel := strings.Index(token[from:], name)
		if rel < 0 {
			return -1
		}
		i := from + rel
		end := i + len(name)
		beforeOK := i == 0 || token[i-1] == '/' || token[i-1] == '\\'
		afterOK := end == len(token) || token[end] == '/' || token[end] == '\\'
		if beforeOK && afterOK {
			return end
		}
		from = i + 1
	}
	return -1
}

func homePrefixLen(token string) int {
	for _, prefix := range []string{"/Users/", "/home/"} {
		if !strings.HasPrefix(token, prefix) {
			continue
		}
		userEnd := userSegmentEnd(token[len(prefix):])
		if userEnd <= 0 {
			return 0
		}
		return len(prefix) + userEnd
	}
	return windowsHomePrefixLen(token)
}

func windowsHomePrefixLen(token string) int {
	if !isAbsolutePathToken(token) || token[0] == '/' {
		return 0
	}
	i := 2
	slashes := leadingPathSepLen(token[i:])
	if slashes == 0 || !strings.HasPrefix(token[i+slashes:], "Users") {
		return 0
	}
	i += slashes + len("Users")
	slashes = leadingPathSepLen(token[i:])
	if slashes == 0 {
		return 0
	}
	i += slashes
	userEnd := userSegmentEnd(token[i:])
	if userEnd <= 0 {
		return 0
	}
	return i + userEnd
}

func userSegmentEnd(rest string) int {
	if rest == "" {
		return 0
	}
	i := 0
	for i < len(rest) && rest[i] != '/' && rest[i] != '\\' {
		i++
	}
	return i
}

func leadingPathSepLen(s string) int {
	if strings.HasPrefix(s, `\\`) {
		return 2
	}
	if strings.HasPrefix(s, `\`) || strings.HasPrefix(s, "/") {
		return 1
	}
	return 0
}

func hasPathComponent(token, name string) bool {
	return slices.Contains(pathComponents(token), name)
}

func hasDotfileComponent(token string) bool {
	for _, part := range pathComponents(token) {
		if len(part) > 1 && part[0] == '.' && part != ".." {
			return true
		}
	}
	return false
}

func pathComponents(token string) []string {
	return strings.FieldsFunc(token, func(r rune) bool {
		return r == '/' || r == '\\'
	})
}
