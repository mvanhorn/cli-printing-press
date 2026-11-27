package appsniff

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/andybalholm/brotli"
)

const (
	simpleCacheInitialMagic uint64 = 0xfcfb6d1ba7725c30
	simpleCacheEOFMagic     uint64 = 0xf4fa6f45970d41d8
	simpleCacheHeaderBytes         = 24
)

var (
	simpleCacheEOFMagicLE = []byte{0xd8, 0x41, 0x0d, 0x97, 0x45, 0x6f, 0xfa, 0xf4}
	hashedStemPattern     = regexp.MustCompile(`(?i)([._-])[0-9a-f]{7,32}(\.(?:js|mjs|cjs|json))$`)
)

// SimpleCacheEntry holds the decoded URL key and payload from a Chromium
// SimpleCache v5 file (Service Worker CacheStorage or HTTP Cache_Data).
type SimpleCacheEntry struct {
	Key     string
	Payload []byte
}

// ParseSimpleCacheEntry decodes a Chromium SimpleCache v5 entry file in pure Go.
// It strips the 24-byte header, key bytes, and trailing EOF record, and
// transparently decompresses gzip or brotli payloads when present.
func ParseSimpleCacheEntry(data []byte, maxOutputBytes int64) (SimpleCacheEntry, error) {
	if len(data) < simpleCacheHeaderBytes {
		return SimpleCacheEntry{}, errors.New("file shorter than simplecache header")
	}
	magic := binary.LittleEndian.Uint64(data[0:8])
	if magic != simpleCacheInitialMagic {
		return SimpleCacheEntry{}, errors.New("not a chromium simplecache entry")
	}
	keyLen := int(binary.LittleEndian.Uint32(data[12:16]))
	if keyLen <= 0 || simpleCacheHeaderBytes+keyLen > len(data) {
		return SimpleCacheEntry{}, errors.New("invalid simplecache key length")
	}

	rawKey := string(data[simpleCacheHeaderBytes : simpleCacheHeaderBytes+keyLen])
	key := extractSimpleCacheURL(rawKey)

	body := data[simpleCacheHeaderBytes+keyLen:]
	if idx := bytes.Index(body, simpleCacheEOFMagicLE); idx >= 0 {
		body = body[:idx]
	}

	payload := maybeDecompressCachePayload(body, maxOutputBytes)
	return SimpleCacheEntry{
		Key:     SanitizeURL(key),
		Payload: payload,
	}, nil
}

func extractSimpleCacheURL(rawKey string) string {
	// Chromium double-keyed CacheStorage keys embed a `_dk_<origin> <resource-url>`
	// prefix, so take the last URL occurrence to get the actual cached asset URL.
	if idx := strings.LastIndex(rawKey, "https://"); idx >= 0 {
		return strings.TrimRight(rawKey[idx:], "\x00 \r\n\t")
	}
	if idx := strings.LastIndex(rawKey, "http://"); idx >= 0 {
		return strings.TrimRight(rawKey[idx:], "\x00 \r\n\t")
	}
	return strings.TrimRight(rawKey, "\x00 \r\n\t")
}

func maybeDecompressCachePayload(raw []byte, maxOutputBytes int64) []byte {
	if len(raw) == 0 {
		return nil
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = defaultMaxFileBytes
	}

	// Gzip header: 0x1f 0x8b
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		gr, err := gzip.NewReader(bytes.NewReader(raw))
		if err == nil {
			defer func() { _ = gr.Close() }()
			if out, rerr := io.ReadAll(io.LimitReader(gr, maxOutputBytes)); rerr == nil && len(out) > 0 {
				return out
			}
		}
	}

	// Plain UTF-8 JS/JSON/HTML payloads start with printable ASCII or BOM.
	if looksLikeTextPayload(raw) {
		if int64(len(raw)) > maxOutputBytes {
			return raw[:maxOutputBytes]
		}
		return raw
	}

	// Fallback: try brotli decompression (common in Chromium CacheStorage).
	br := brotli.NewReader(bytes.NewReader(raw))
	if out, err := io.ReadAll(io.LimitReader(br, maxOutputBytes)); err == nil && len(out) > 0 && looksLikeTextPayload(out) {
		return out
	}

	// Some Chromium entries store HTTP response headers before the body in stream 0
	// and payload in stream 1; scan for gzip magic inside stream if small header precedes it.
	if idx := bytes.Index(raw, []byte{0x1f, 0x8b, 0x08}); idx > 0 && idx < 4096 {
		if gr, err := gzip.NewReader(bytes.NewReader(raw[idx:])); err == nil {
			defer func() { _ = gr.Close() }()
			if out, rerr := io.ReadAll(io.LimitReader(gr, maxOutputBytes)); rerr == nil && len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func looksLikeTextPayload(b []byte) bool {
	sample := b
	if len(sample) > 512 {
		sample = sample[:512]
	}
	if len(sample) == 0 {
		return false
	}
	printable := 0
	for _, c := range sample {
		if c == 0 {
			return false
		}
		if (c >= 0x20 && c <= 0x7e) || c == '\n' || c == '\r' || c == '\t' || c >= 0x80 {
			printable++
		}
	}
	return float64(printable)/float64(len(sample)) >= 0.88
}

// DiscoverCacheDir analyzes a Chromium/Electron CacheStorage directory directly.
func DiscoverCacheDir(cacheDir string, opts Options) (*DiscoveryResult, error) {
	absPath, err := filepath.Abs(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("resolving cache dir: %w", err)
	}
	res := &DiscoveryResult{
		Target:     absPath,
		TargetKind: TargetKindCache,
		Meta: BinaryMeta{
			AppName:   filepath.Base(absPath),
			Framework: FrameworkChromiumCache,
		},
		Auth: AuthSignal{Scheme: AuthNone},
	}
	acc := newExtractorAccumulator()
	if err := scanCacheDirInto(absPath, opts, res, acc); err != nil {
		return nil, err
	}
	finalizeDiscoveryResult(res, acc, opts)
	return res, nil
}

func scanCacheDirInto(dir string, opts Options, res *DiscoveryResult, acc *extractorAccumulator) error {
	if IsForbiddenSecretPath(dir) {
		return fmt.Errorf("refusing to scan forbidden secret path: %s", dir)
	}
	maxFile := opts.maxFileBytes()
	maxTotal := opts.maxTotalBytes()

	type candidate struct {
		path    string
		stem    string
		urlKey  string
		modTime int64
		size    int64
	}
	byStem := make(map[string]candidate)
	var directCandidates []candidate

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if IsForbiddenSecretPath(path) {
				res.Stats.FilesSkippedSecret++
				return filepath.SkipDir
			}
			return nil
		}
		if IsForbiddenSecretPath(path) {
			res.Stats.FilesSkippedSecret++
			return nil
		}
		fi, err := d.Info()
		if err != nil || fi.Size() < simpleCacheHeaderBytes || fi.Size() > maxFile {
			return nil
		}

		// Peek header + key first to avoid reading non-JS media entries into memory.
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		var hdr [simpleCacheHeaderBytes]byte
		_, rerr := io.ReadFull(f, hdr[:])
		if rerr != nil {
			_ = f.Close()
			return nil
		}
		if binary.LittleEndian.Uint64(hdr[0:8]) != simpleCacheInitialMagic {
			_ = f.Close()
			return nil
		}
		keyLen := int(binary.LittleEndian.Uint32(hdr[12:16]))
		if keyLen <= 0 || keyLen > 4096 {
			_ = f.Close()
			return nil
		}
		keyBuf := make([]byte, keyLen)
		_, rerr = io.ReadFull(f, keyBuf)
		_ = f.Close()
		if rerr != nil {
			return nil
		}
		urlKey := extractSimpleCacheURL(string(keyBuf))
		if !isScannableCacheURL(urlKey) {
			return nil
		}

		stem := normalizedCacheStem(urlKey)
		cand := candidate{
			path:    path,
			stem:    stem,
			urlKey:  urlKey,
			modTime: fi.ModTime().UnixNano(),
			size:    fi.Size(),
		}
		if stem == "" {
			directCandidates = append(directCandidates, cand)
			return nil
		}
		prev, ok := byStem[stem]
		if !ok || cand.modTime > prev.modTime || (cand.modTime == prev.modTime && cand.size > prev.size) {
			byStem[stem] = cand
		}
		return nil
	})
	if err != nil {
		return err
	}

	selected := make([]candidate, 0, len(byStem)+len(directCandidates))
	for _, c := range byStem {
		selected = append(selected, c)
	}
	selected = append(selected, directCandidates...)

	var total int64
	for _, c := range selected {
		if total+c.size > maxTotal {
			break
		}
		raw, err := os.ReadFile(c.path)
		if err != nil {
			continue
		}
		entry, err := ParseSimpleCacheEntry(raw, maxFile)
		if err != nil || len(entry.Payload) == 0 {
			continue
		}
		total += int64(len(entry.Payload))
		res.Stats.FilesScanned++
		if u, uerr := url.Parse(entry.Key); uerr == nil && u.Scheme != "" && u.Host != "" {
			acc.addBaseURL(u.Scheme + "://" + u.Host)
		}
		acc.scanJS(c.stem, string(entry.Payload), "cache:"+SanitizeURL(entry.Key))
	}
	return nil
}

func isScannableCacheURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	p := strings.ToLower(u.Path)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".otf", ".mp3", ".mp4", ".wav", ".css", ".map", ".wasm"} {
		if strings.HasSuffix(p, ext) {
			return false
		}
	}
	for _, ext := range []string{".js", ".mjs", ".cjs", ".json"} {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return strings.Contains(p, "/bundle") || strings.Contains(p, "/chunk")
}

func normalizedCacheStem(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	base := filepath.Base(u.Path)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	return hashedStemPattern.ReplaceAllString(base, "$2")
}
