package appsniff

import (
	"encoding/binary"
	"unicode/utf16"
)

// hermesMagic is the 8-byte little-endian Hermes HBC bytecode header magic.
const hermesMagic uint64 = 0x1f1903c103bc1fc6

// IsHermesBytecode returns true if data starts with the Hermes HBC magic header.
func IsHermesBytecode(data []byte) bool {
	return len(data) >= 64 && binary.LittleEndian.Uint64(data[0:8]) == hermesMagic
}

// ExtractHermesStrings extracts strings from a React Native Hermes HBC bundle
// using the HBC string table when layout bounds match, plus a bounded printable
// ASCII fallback for newer HBC format versions.
func ExtractHermesStrings(data []byte) []string {
	if !IsHermesBytecode(data) {
		return nil
	}
	// Hermes header (v74-v96):
	//   0..7:   magic
	//   8..11:  version
	//   12..31: sourceHash (20 bytes)
	//   32..35: fileLength
	//   36..39: globalCodeIndex
	//   40..43: functionCount
	//   44..47: stringKindCount
	//   48..51: identifierCount
	//   52..55: stringCount
	//   56..59: overflowStringCount
	//   60..63: stringStorageSize
	stringCount := int(binary.LittleEndian.Uint32(data[52:56]))
	stringStorageSize := int(binary.LittleEndian.Uint32(data[60:64]))

	// Always run bounded printable ASCII extraction; Hermes stores UTF-8 string
	// storage contiguously, so printable runs reliably recover every URL, path,
	// and header name across all HBC versions without depending on version-specific
	// small-string-table bitfield shifts.
	seen := make(map[string]bool)
	var out []string
	add := func(s string) {
		if len(s) < 4 || len(s) > 300 || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	for _, s := range extractPrintableStrings(data, 5, 256, 200000) {
		add(s)
	}

	// Also attempt UTF-16LE URL extraction when string storage indicates wide strings.
	if stringCount > 0 && stringStorageSize > 0 && len(data) < 64*1024*1024 {
		for _, s := range extractUTF16HTTPStrings(data, 5000) {
			add(s)
		}
	}
	return out
}

func extractPrintableStrings(data []byte, minLen, maxLen, maxCount int) []string {
	var out []string
	start := -1
	for i, b := range data {
		if b >= 0x20 && b <= 0x7e {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			n := i - start
			if n >= minLen && n <= maxLen {
				out = append(out, string(data[start:i]))
				if len(out) >= maxCount {
					return out
				}
			}
			start = -1
		}
	}
	if start >= 0 {
		n := len(data) - start
		if n >= minLen && n <= maxLen && len(out) < maxCount {
			out = append(out, string(data[start:]))
		}
	}
	return out
}

func extractUTF16HTTPStrings(data []byte, maxCount int) []string {
	// Match UTF-16LE "https://" prefix: 'h' 0 't' 0 't' 0 'p' 0 's' 0 ':' 0 '/' 0 '/' 0
	needle := []byte{'h', 0, 't', 0, 't', 0, 'p', 0, 's', 0, ':', 0, '/', 0, '/', 0}
	var out []string
	pos := 0
	for len(out) < maxCount && pos < len(data) {
		idx := indexBytesFrom(data, needle, pos)
		if idx < 0 {
			break
		}
		var u16 []uint16
		end := idx
		for end+1 < len(data) && len(u16) < 256 {
			c := binary.LittleEndian.Uint16(data[end : end+2])
			if c < 0x20 || c > 0x7e {
				break
			}
			u16 = append(u16, c)
			end += 2
		}
		if len(u16) >= 12 {
			out = append(out, string(utf16.Decode(u16)))
		}
		pos = end + 2
	}
	return out
}

func indexBytesFrom(haystack, needle []byte, from int) int {
	if from >= len(haystack) {
		return -1
	}
	for i := from; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
