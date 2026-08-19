package pipeline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

var (
	specLF   = []byte("\n")
	specCRLF = []byte("\r\n")
)

// ComputeSpecChecksum returns the canonical SHA-256 checksum for specBytes.
// Known text spec formats normalize CRLF line endings to LF before hashing so
// the checksum does not depend on the checkout platform. Other formats retain
// their byte-for-byte checksum semantics.
func ComputeSpecChecksum(specBytes []byte, specFormat string) string {
	return specChecksumCandidates(specBytes, specFormat)[0]
}

// SpecChecksumMatches reports whether checksum is the canonical checksum or a
// legacy raw-byte checksum for specBytes. During the line-ending transition,
// text specs accept both all-LF and all-CRLF legacy hashes so a manifest written
// on either platform keeps its lineage when regenerated on the other.
func SpecChecksumMatches(checksum string, specBytes []byte, specFormat string) bool {
	return slices.Contains(specChecksumCandidates(specBytes, specFormat), checksum)
}

func specChecksumCandidates(specBytes []byte, specFormat string) []string {
	raw := hashSpecBytes(specBytes)
	if !normalizesSpecLineEndings(specFormat) {
		return []string{raw}
	}

	lfBytes := bytes.ReplaceAll(specBytes, specCRLF, specLF)
	candidates := []string{hashSpecBytes(lfBytes)}
	candidates = appendUniqueSpecChecksum(candidates, raw)

	// A legacy manifest may have been written from the other checkout style.
	// Reconstruct the all-CRLF form from canonical LF bytes so both directions
	// of a Windows/Linux regeneration recognize the same spec lineage.
	crlfBytes := bytes.ReplaceAll(lfBytes, specLF, specCRLF)
	return appendUniqueSpecChecksum(candidates, hashSpecBytes(crlfBytes))
}

func appendUniqueSpecChecksum(checksums []string, candidate string) []string {
	if slices.Contains(checksums, candidate) {
		return checksums
	}
	return append(checksums, candidate)
}

func hashSpecBytes(specBytes []byte) string {
	sum := sha256.Sum256(specBytes)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizesSpecLineEndings(specFormat string) bool {
	switch strings.ToLower(strings.TrimSpace(specFormat)) {
	case "openapi3", "graphql", "internal", spec.SourceLocalSQLite:
		return true
	default:
		return false
	}
}
