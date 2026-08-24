package pipeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// PatchRecord is one customization recorded under PatchesDirName or in the
// legacy PatchesIndexFilename array. Regen and publish-validate read these
// back so a record cannot claim a customization shipped after its files or
// declared call sites disappeared.
type PatchRecord struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	Files         []string `json:"files,omitempty"`
	Marker        string   `json:"marker,omitempty"`
	Markers       []string `json:"markers,omitempty"`
	CallSites     []string `json:"call_sites,omitempty"`

	// Source is the relative path of the file that produced this record.
	Source string `json:"-"`
}

// LoadPatchRecords reads every applied patch from dir. The per-patch
// directory is preferred; the legacy single-array file is also read so older
// published CLIs stay visible. _meta.json is directory-level metadata and is
// not an applied patch. Missing indexes yield a nil slice, not an error.
func LoadPatchRecords(dir string) ([]PatchRecord, error) {
	var records []PatchRecord
	seen := map[string]struct{}{}

	patchesDir := filepath.Join(dir, PatchesDirName)
	entries, err := os.ReadDir(patchesDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading %s: %w", PatchesDirName, err)
	}
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || name == PatchesGitKeepName || name == PatchesMetadataFilename {
				continue
			}
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			rel := filepath.ToSlash(filepath.Join(PatchesDirName, name))
			rec, err := loadPatchRecordFile(filepath.Join(patchesDir, name), rel)
			if err != nil {
				return nil, err
			}
			if rec.ID == "" {
				rec.ID = strings.TrimSuffix(name, ".json")
			}
			seen[rec.ID] = struct{}{}
			records = append(records, rec)
		}
	}

	legacy, err := loadLegacyPatchRecords(dir)
	if err != nil {
		return nil, err
	}
	for _, rec := range legacy {
		if rec.ID != "" {
			if _, dup := seen[rec.ID]; dup {
				continue
			}
			seen[rec.ID] = struct{}{}
		}
		records = append(records, rec)
	}

	slices.SortFunc(records, func(a, b PatchRecord) int {
		return strings.Compare(a.ID, b.ID)
	})
	return records, nil
}

func loadPatchRecordFile(path, rel string) (PatchRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PatchRecord{}, fmt.Errorf("reading patch record %s: %w", rel, err)
	}
	rec, err := decodePatchRecord(data)
	if err != nil {
		return PatchRecord{}, fmt.Errorf("reading patch record %s: %w", rel, err)
	}
	rec.Source = rel
	return rec, nil
}

func decodePatchRecord(data []byte) (PatchRecord, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return PatchRecord{}, errors.New("empty JSON")
	}
	var rec PatchRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return PatchRecord{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return rec, nil
}

func supportedPatchSchema(version int) bool {
	return version == 1 || version == CurrentPatchesIndexSchemaVersion
}

func patchRecordSchemaViolation(rec PatchRecord, id string) string {
	version := rec.SchemaVersion
	if rec.Source == PatchesIndexFilename && version == 0 {
		return ""
	}
	if supportedPatchSchema(version) {
		return ""
	}
	if version == 0 {
		return fmt.Sprintf("patch %q: schema_version is required (supported: 1, %d)", id, CurrentPatchesIndexSchemaVersion)
	}
	return fmt.Sprintf("patch %q: unsupported schema_version %d (supported: 1, %d)", id, version, CurrentPatchesIndexSchemaVersion)
}

func loadLegacyPatchRecords(dir string) ([]PatchRecord, error) {
	path := filepath.Join(dir, PatchesIndexFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", PatchesIndexFilename, err)
	}
	var index PatchesIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("reading %s: %w", PatchesIndexFilename, err)
	}
	records := make([]PatchRecord, 0, len(index.Patches))
	for i, raw := range index.Patches {
		rec, err := decodePatchRecord(raw)
		if err != nil {
			return nil, fmt.Errorf("reading %s patches[%d]: %w", PatchesIndexFilename, i, err)
		}
		if rec.ID == "" {
			rec.ID = fmt.Sprintf("patches[%d]", i)
		}
		rec.Source = PatchesIndexFilename
		records = append(records, rec)
	}
	return records, nil
}

// ValidatePatchRecords fails closed when a recorded customization is no
// longer present in dir. A missing or empty patches index passes. A record
// naming a missing file, declaring no files or call sites, or declaring a
// marker / call site absent from its recorded files, fails and names the
// patch id. Needles are checked only in files[] when that list is present
// so an unrelated leftover substring cannot mask a dropped call site.
// files[] is required for call_sites and non-pp:patch markers; tree-wide
// search is only used for unique pp:patch markers. Per-patch files must
// declare schema_version 1 or CurrentPatchesIndexSchemaVersion; omitted
// schema is accepted only on legacy index entries.
func ValidatePatchRecords(dir string) error {
	records, err := LoadPatchRecords(dir)
	if err != nil {
		return err
	}
	var violations []string
	for _, rec := range records {
		violations = append(violations, patchRecordViolations(dir, rec)...)
	}
	if len(violations) == 0 {
		return nil
	}
	return errors.New(strings.Join(violations, "; "))
}

func patchRecordViolations(dir string, rec PatchRecord) []string {
	id := rec.ID
	if id == "" {
		id = rec.Source
	}
	if msg := patchRecordSchemaViolation(rec, id); msg != "" {
		return []string{msg}
	}
	needles := recNeedles(rec)
	var listed []string
	var violations []string
	for _, raw := range rec.Files {
		rel, err := safePatchRelPath(raw)
		if err != nil {
			violations = append(violations, fmt.Sprintf("patch %q: recorded file %q is not a safe relative path", id, raw))
			continue
		}
		listed = append(listed, rel)
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			violations = append(violations, fmt.Sprintf("patch %q: recorded file %q is missing", id, rel))
		}
	}
	if len(listed) == 0 && len(needles) == 0 {
		return append(violations, fmt.Sprintf("patch %q: record declares no files or call sites", id))
	}
	for _, needle := range needles {
		if len(listed) == 0 && !isPatchMarkerNeedle(needle) {
			violations = append(violations, fmt.Sprintf("patch %q: call site %q requires files[] so leftover matches cannot mask a drop", id, needle))
			continue
		}
		var found bool
		var err error
		scope := "the tree"
		if len(listed) > 0 {
			found, err = listedFilesContainNeedle(dir, listed, needle)
			scope = "recorded files"
		} else {
			found, err = treeContainsPatchNeedle(dir, needle)
		}
		if err != nil {
			violations = append(violations, fmt.Sprintf("patch %q: searching for %q: %v", id, needle, err))
			continue
		}
		if !found {
			violations = append(violations, fmt.Sprintf("patch %q: recorded call site %q is absent from %s", id, needle, scope))
		}
	}
	return violations
}

func isPatchMarkerNeedle(needle string) bool {
	return strings.HasPrefix(strings.TrimSpace(needle), "pp:patch")
}

func recNeedles(rec PatchRecord) []string {
	var needles []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if slices.Contains(needles, s) {
			return
		}
		needles = append(needles, s)
	}
	add(rec.Marker)
	for _, s := range rec.Markers {
		add(s)
	}
	for _, s := range rec.CallSites {
		add(s)
	}
	return needles
}

func safePatchRelPath(raw string) (string, error) {
	rel := filepath.ToSlash(strings.TrimSpace(raw))
	rel = strings.TrimPrefix(rel, "./")
	if rel == "" || rel == "." {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(rel, "/") {
		return "", errors.New("absolute path")
	}
	if slices.Contains(strings.Split(rel, "/"), "..") {
		return "", errors.New("parent segment")
	}
	return rel, nil
}

func listedFilesContainNeedle(dir string, rels []string, needle string) (bool, error) {
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, err
		}
		if bytes.Contains(data, []byte(needle)) {
			return true, nil
		}
	}
	return false, nil
}

func treeContainsPatchNeedle(dir, needle string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			if skipPatchSearchDir(d.Name(), relSlash) {
				return filepath.SkipDir
			}
			return nil
		}
		if skipPatchSearchFile(relSlash) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(needle)) {
			found = true
			return errPatchNeedleFound
		}
		return nil
	})
	if errors.Is(err, errPatchNeedleFound) {
		return true, nil
	}
	return found, err
}

var errPatchNeedleFound = errors.New("patch needle found")

func skipPatchSearchDir(name, relSlash string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "build", "dist", ".gotmp":
		return true
	case PatchesDirName, ".manuscripts":
		return true
	}
	return relSlash == PatchesDirName || strings.HasPrefix(relSlash, PatchesDirName+"/")
}

func skipPatchSearchFile(relSlash string) bool {
	base := filepath.Base(relSlash)
	if base == PatchesIndexFilename {
		return true
	}
	if strings.HasPrefix(relSlash, PatchesDirName+"/") {
		return true
	}
	if strings.HasSuffix(relSlash, "_test.go") {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".md", ".txt":
		return true
	}
	return false
}

// PreservePatchRecords copies snapshot patch records into dst so generate
// --force / MergeIntoFreshTree cannot drop the index. Hidden-dir sweep skips
// PatchesDirName; this is the dedicated reader-side preserve. Destination
// files already present are left untouched so a fresh print's .gitkeep stays
// put; snapshot-only <id>.json files are copied.
func PreservePatchRecords(srcDir, dstDir string) error {
	if err := preservePatchesDir(srcDir, dstDir); err != nil {
		return err
	}
	return preserveLegacyPatchIndex(srcDir, dstDir)
}

func preservePatchesDir(srcDir, dstDir string) error {
	src := filepath.Join(srcDir, PatchesDirName)
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading snapshot %s: %w", PatchesDirName, err)
	}
	dst := filepath.Join(dstDir, PatchesDirName)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", PatchesDirName, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == PatchesGitKeepName {
			continue
		}
		srcPath := filepath.Join(src, entry.Name())
		info, err := os.Lstat(srcPath)
		if err != nil {
			return fmt.Errorf("statting snapshot patch %s: %w", entry.Name(), err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to preserve symlinked patch record: %s", entry.Name())
		}
		dstPath := filepath.Join(dst, entry.Name())
		if _, err := os.Lstat(dstPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("statting destination patch %s: %w", entry.Name(), err)
		}
		if err := copyFile(srcPath, dstPath, info.Mode().Perm()); err != nil {
			return fmt.Errorf("preserving patch record %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func preserveLegacyPatchIndex(srcDir, dstDir string) error {
	src := filepath.Join(srcDir, PatchesIndexFilename)
	info, err := os.Lstat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("statting snapshot %s: %w", PatchesIndexFilename, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to preserve symlinked %s", PatchesIndexFilename)
	}
	dst := filepath.Join(dstDir, PatchesIndexFilename)
	if _, err := os.Lstat(dst); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("statting destination %s: %w", PatchesIndexFilename, err)
	}
	if err := copyFile(src, dst, info.Mode().Perm()); err != nil {
		return fmt.Errorf("preserving %s: %w", PatchesIndexFilename, err)
	}
	return nil
}
