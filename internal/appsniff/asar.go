package appsniff

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	maxASARHeaderBytes = 32 * 1024 * 1024
)

// ASARFile represents an extracted text/JS/JSON entry from an Electron app.asar archive.
type ASARFile struct {
	Path string
	Data []byte
}

type asarNode struct {
	Files      map[string]asarNode `json:"files,omitempty"`
	Size       int64               `json:"size,omitempty"`
	Offset     string              `json:"offset,omitempty"`
	Unpacked   bool                `json:"unpacked,omitempty"`
	Executable bool                `json:"executable,omitempty"`
}

// ExtractASAR reads an Electron ASAR archive using Chromium's pickle framing
// and returns matching JS/TS/JSON/HTML entries within bounded memory limits.
func ExtractASAR(path string, opts Options) ([]ASARFile, error) {
	if IsForbiddenSecretPath(path) {
		return nil, fmt.Errorf("refusing to read forbidden secret path: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening asar %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var prefix [16]byte
	if _, err := io.ReadFull(f, prefix[:]); err != nil {
		return nil, fmt.Errorf("reading asar pickle header: %w", err)
	}
	// Chromium Pickle layout:
	//   [0..3]  outer pickle payload size (always 4)
	//   [4..7]  header pickle size
	//   [8..11] header pickle payload size
	//   [12..15] header JSON string byte length
	outerSize := binary.LittleEndian.Uint32(prefix[0:4])
	headerPickleSize := binary.LittleEndian.Uint32(prefix[4:8])
	jsonLen := binary.LittleEndian.Uint32(prefix[12:16])

	if outerSize != 4 || headerPickleSize == 0 || jsonLen == 0 || jsonLen > maxASARHeaderBytes || headerPickleSize > maxASARHeaderBytes {
		return nil, fmt.Errorf("invalid asar pickle header in %s", path)
	}

	headerBytes := make([]byte, jsonLen)
	if _, err := io.ReadFull(f, headerBytes); err != nil {
		return nil, fmt.Errorf("reading asar json header: %w", err)
	}

	var root asarNode
	if err := json.Unmarshal(headerBytes, &root); err != nil {
		return nil, fmt.Errorf("parsing asar json header: %w", err)
	}

	dataOffset := int64(8) + int64(headerPickleSize)
	maxFile := opts.maxFileBytes()
	maxTotal := opts.maxTotalBytes()

	type entryRef struct {
		relPath string
		offset  int64
		size    int64
	}
	var refs []entryRef
	var walk func(prefixPath string, node asarNode)
	walk = func(prefixPath string, node asarNode) {
		names := make([]string, 0, len(node.Files))
		for name := range node.Files {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			child := node.Files[name]
			rel := name
			if prefixPath != "" {
				rel = prefixPath + "/" + name
			}
			if len(child.Files) > 0 {
				if shouldSkipWalkDir(name) {
					continue
				}
				walk(rel, child)
				continue
			}
			if child.Unpacked || child.Size <= 0 || child.Size > maxFile {
				continue
			}
			if !isScannableCodeAsset(rel) || IsForbiddenSecretPath(rel) {
				continue
			}
			off, err := strconv.ParseInt(child.Offset, 10, 64)
			if err != nil || off < 0 {
				continue
			}
			refs = append(refs, entryRef{relPath: rel, offset: dataOffset + off, size: child.Size})
		}
	}
	walk("", root)

	var out []ASARFile
	var totalRead int64
	for _, ref := range refs {
		if totalRead+ref.size > maxTotal {
			break
		}
		buf := make([]byte, ref.size)
		if _, err := f.ReadAt(buf, ref.offset); err != nil {
			continue
		}
		totalRead += ref.size
		out = append(out, ASARFile{Path: ref.relPath, Data: buf})
	}
	return out, nil
}

func shouldSkipWalkDir(name string) bool {
	switch strings.ToLower(name) {
	case "node_modules", ".git", "locales", "i18n", "fonts", "images", "icons", "assets-images", "__tests__", "test", "tests":
		return true
	default:
		return false
	}
}

func isScannableCodeAsset(rel string) bool {
	lower := strings.ToLower(rel)
	if strings.HasSuffix(lower, ".d.ts") || strings.HasSuffix(lower, ".map") {
		return false
	}
	for _, ext := range []string{".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx", ".json", ".html"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// PlistInfo holds non-sensitive metadata parsed from an Apple Info.plist file.
type PlistInfo struct {
	AppName     string
	BundleID    string
	Version     string
	BuildNumber string
	ATSDomains  []string
	URLSchemes  []string
}

// ParseInfoPlist parses XML Info.plist files directly in pure Go, and falls back
// to macOS plutil when encountering binary bplist00 payloads.
func ParseInfoPlist(data []byte) (PlistInfo, error) {
	if len(data) == 0 {
		return PlistInfo{}, errors.New("empty plist")
	}
	xmlData := data
	if bytes.HasPrefix(data, []byte("bplist")) {
		converted, err := convertBinaryPlistToXML(data)
		if err != nil {
			return PlistInfo{}, err
		}
		xmlData = converted
	}

	root, err := parseXMLPlist(xmlData)
	if err != nil {
		return PlistInfo{}, err
	}
	dict, ok := root.(map[string]any)
	if !ok {
		return PlistInfo{}, errors.New("plist root is not a dict")
	}

	info := PlistInfo{
		AppName:     firstNonEmptyString(dict, "CFBundleDisplayName", "CFBundleName", "CFBundleExecutable"),
		BundleID:    firstNonEmptyString(dict, "CFBundleIdentifier"),
		Version:     firstNonEmptyString(dict, "CFBundleShortVersionString", "CFBundleVersion"),
		BuildNumber: firstNonEmptyString(dict, "CFBundleVersion"),
	}

	if ats, ok := dict["NSAppTransportSecurity"].(map[string]any); ok {
		if domains, ok := ats["NSExceptionDomains"].(map[string]any); ok {
			for d := range domains {
				d = strings.TrimSpace(strings.ToLower(d))
				if d != "" && strings.Contains(d, ".") {
					info.ATSDomains = append(info.ATSDomains, d)
				}
			}
			sort.Strings(info.ATSDomains)
		}
	}

	if urlTypes, ok := dict["CFBundleURLTypes"].([]any); ok {
		seen := make(map[string]bool)
		for _, item := range urlTypes {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			schemes, ok := m["CFBundleURLSchemes"].([]any)
			if !ok {
				continue
			}
			for _, sc := range schemes {
				if s, ok := sc.(string); ok && s != "" && !seen[s] {
					seen[s] = true
					info.URLSchemes = append(info.URLSchemes, s)
				}
			}
		}
		sort.Strings(info.URLSchemes)
	}

	return info, nil
}

func firstNonEmptyString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func convertBinaryPlistToXML(data []byte) ([]byte, error) {
	plutil, err := exec.LookPath("plutil")
	if err != nil {
		return nil, fmt.Errorf("binary plist detected and plutil is unavailable: %w", err)
	}
	cmd := exec.Command(plutil, "-convert", "xml1", "-o", "-", "--", "-")
	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("plutil conversion failed: %w", err)
	}
	return out.Bytes(), nil
}

func parseXMLPlist(data []byte) (any, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "plist" {
			return decodePlistNextValue(dec)
		}
	}
}

func decodePlistNextValue(dec *xml.Decoder) (any, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return decodePlistElement(dec, t)
		case xml.EndElement:
			return nil, nil
		}
	}
}

func decodePlistElement(dec *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "dict":
		out := make(map[string]any)
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.EndElement:
				if t.Name.Local == "dict" {
					return out, nil
				}
			case xml.StartElement:
				if t.Name.Local != "key" {
					if _, err := decodePlistElement(dec, t); err != nil {
						return nil, err
					}
					continue
				}
				var key string
				if err := dec.DecodeElement(&key, &t); err != nil {
					return nil, err
				}
				val, err := decodePlistNextValue(dec)
				if err != nil {
					return nil, err
				}
				out[key] = val
			}
		}
	case "array":
		var out []any
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.EndElement:
				if t.Name.Local == "array" {
					return out, nil
				}
			case xml.StartElement:
				val, err := decodePlistElement(dec, t)
				if err != nil {
					return nil, err
				}
				out = append(out, val)
			}
		}
	case "string", "date", "data":
		var s string
		if err := dec.DecodeElement(&s, &start); err != nil {
			return nil, err
		}
		return s, nil
	case "integer":
		var s string
		if err := dec.DecodeElement(&s, &start); err != nil {
			return nil, err
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		return n, nil
	case "real":
		var s string
		if err := dec.DecodeElement(&s, &start); err != nil {
			return nil, err
		}
		f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return f, nil
	case "true":
		_ = dec.Skip()
		return true, nil
	case "false":
		_ = dec.Skip()
		return false, nil
	default:
		_ = dec.Skip()
		return nil, nil
	}
}

// DiscoverDesktopApp analyzes a macOS .app bundle, standalone .asar file, or
// source directory, and optionally inspects the matching Electron Service Worker
// CacheStorage directory for thin-shell desktop apps.
func DiscoverDesktopApp(targetPath string, opts Options) (*DiscoveryResult, error) {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", absPath, err)
	}

	res := &DiscoveryResult{
		Target:     absPath,
		TargetKind: TargetKindDesktop,
		Meta: BinaryMeta{
			Platform:  runtime.GOOS,
			Framework: FrameworkElectron,
		},
		Auth: AuthSignal{Scheme: AuthNone},
	}

	acc := newExtractorAccumulator()

	switch {
	case !info.IsDir() && strings.HasSuffix(strings.ToLower(absPath), ".asar"):
		res.Meta.AppName = strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
		files, err := ExtractASAR(absPath, opts)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			res.Stats.FilesScanned++
			acc.scanJS(f.Path, string(f.Data), "asar:"+f.Path)
		}

	case info.IsDir() && strings.HasSuffix(strings.ToLower(absPath), ".app"):
		res.Meta.AppName = strings.TrimSuffix(filepath.Base(absPath), ".app")
		plistPath := filepath.Join(absPath, "Contents", "Info.plist")
		if plistBytes, err := os.ReadFile(plistPath); err == nil {
			if pinfo, perr := ParseInfoPlist(plistBytes); perr == nil {
				if pinfo.AppName != "" {
					res.Meta.AppName = pinfo.AppName
				}
				res.Meta.BundleID = pinfo.BundleID
				res.Meta.Version = pinfo.Version
				res.Meta.BuildNumber = pinfo.BuildNumber
				res.Meta.ATSDomains = pinfo.ATSDomains
				for _, d := range pinfo.ATSDomains {
					acc.addBaseURL("https://" + d)
				}
			}
		}

		resourcesDir := filepath.Join(absPath, "Contents", "Resources")
		asarMatches, _ := filepath.Glob(filepath.Join(resourcesDir, "*.asar"))
		sort.Strings(asarMatches)
		for _, asarPath := range asarMatches {
			files, aerr := ExtractASAR(asarPath, opts)
			if aerr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("asar read warning (%s): %v", filepath.Base(asarPath), aerr))
				continue
			}
			for _, f := range files {
				res.Stats.FilesScanned++
				acc.scanJS(f.Path, string(f.Data), "asar:"+filepath.Base(asarPath)+":"+f.Path)
			}
		}

		// Also scan unpacked Resources/app if present.
		unpackedApp := filepath.Join(resourcesDir, "app")
		if st, serr := os.Stat(unpackedApp); serr == nil && st.IsDir() {
			scanCodeDirectory(unpackedApp, opts, res, acc)
		}

		// Thin-shell Electron apps (e.g. web wrappers) serve main bundles into
		// Chromium Service Worker CacheStorage; scan it automatically when enabled.
		if opts.ScanCache {
			cacheDirs := discoverMacCacheDirs(res.Meta.AppName, strings.TrimSuffix(filepath.Base(absPath), ".app"), opts.CacheDir)
			for _, cdir := range cacheDirs {
				if err := scanCacheDirInto(cdir, opts, res, acc); err != nil {
					res.Warnings = append(res.Warnings, fmt.Sprintf("cache scan warning (%s): %v", cdir, err))
				}
			}
		}

	case info.IsDir():
		res.TargetKind = TargetKindSourceDir
		res.Meta.AppName = filepath.Base(absPath)
		res.Meta.Framework = FrameworkUnknown
		scanCodeDirectory(absPath, opts, res, acc)
		if opts.CacheDir != "" {
			if err := scanCacheDirInto(opts.CacheDir, opts, res, acc); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("cache scan warning (%s): %v", opts.CacheDir, err))
			}
		}

	default:
		res.TargetKind = TargetKindSourceDir
		res.Meta.AppName = strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
		if IsForbiddenSecretPath(absPath) {
			return nil, fmt.Errorf("refusing to read forbidden secret path: %s", absPath)
		}
		data, err := readBoundedFile(absPath, opts.maxFileBytes())
		if err != nil {
			return nil, err
		}
		res.Stats.FilesScanned++
		acc.scanJS(filepath.Base(absPath), string(data), "file:"+filepath.Base(absPath))
	}

	finalizeDiscoveryResult(res, acc, opts)
	return res, nil
}

func scanCodeDirectory(dir string, opts Options, res *DiscoveryResult, acc *extractorAccumulator) {
	maxFile := opts.maxFileBytes()
	maxTotal := opts.maxTotalBytes()
	var total int64

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && (shouldSkipWalkDir(d.Name()) || IsForbiddenSecretPath(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if IsForbiddenSecretPath(path) {
			res.Stats.FilesSkippedSecret++
			return nil
		}
		if !isScannableCodeAsset(path) {
			return nil
		}
		fi, err := d.Info()
		if err != nil || fi.Size() <= 0 || fi.Size() > maxFile {
			return nil
		}
		if total+fi.Size() > maxTotal {
			return filepath.SkipAll
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		total += int64(len(data))
		res.Stats.FilesScanned++
		rel, _ := filepath.Rel(dir, path)
		acc.scanJS(rel, string(data), "dir:"+filepath.ToSlash(rel))
		return nil
	})
}

func discoverMacCacheDirs(appName, bundleBase, explicit string) []string {
	var candidates []string
	if strings.TrimSpace(explicit) != "" {
		candidates = append(candidates, explicit)
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		names := uniqueNonEmpty(appName, bundleBase)
		for _, n := range names {
			base := filepath.Join(home, "Library", "Application Support", n)
			for _, sub := range []string{
				filepath.Join(base, "Service Worker", "CacheStorage"),
				filepath.Join(base, "Cache", "Cache_Data"),
			} {
				if st, serr := os.Stat(sub); serr == nil && st.IsDir() {
					candidates = append(candidates, sub)
				}
			}
		}
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if !slices.Contains(out, abs) {
			out = append(out, abs)
		}
	}
	return out
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, limit))
}

func uniqueNonEmpty(vals ...string) []string {
	var out []string
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || slices.Contains(out, v) {
			continue
		}
		out = append(out, v)
	}
	return out
}
