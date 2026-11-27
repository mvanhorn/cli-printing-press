package onboard

import (
	"database/sql"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/registry"
	_ "modernc.org/sqlite"
)

// BrowserEngine identifies the SQLite schema family of a local browser profile.
type BrowserEngine string

const (
	EngineChromium BrowserEngine = "chromium"
	EngineFirefox  BrowserEngine = "firefox"
)

// BrowserSource points to one local browser profile's Cookies and History files.
type BrowserSource struct {
	Name        string
	Engine      BrowserEngine
	CookiesPath string
	HistoryPath string
}

// DomainSignal summarizes non-sensitive local usage signals for a single domain.
// Only plaintext hostnames, session counts, and visit counts are ever read;
// cookie values and full URLs are never inspected or returned.
type DomainSignal struct {
	Domain      string   `json:"domain"`
	HasSession  bool     `json:"has_session"`
	CookieCount int      `json:"cookie_count"`
	VisitCount  int      `json:"visit_count"`
	Browsers    []string `json:"browsers"`
}

// AppRecommendation maps a user's local browser usage to an immediately usable
// Printing Press Cartridge or prebuilt Printing Press Library CLI.
type AppRecommendation struct {
	App           string   `json:"app"`
	Kind          string   `json:"kind"` // "cartridge" or "prebuilt-binary"
	Category      string   `json:"category,omitempty"`
	MatchedDomain string   `json:"matched_domain"`
	HasSession    bool     `json:"has_session"`
	CookieCount   int      `json:"cookie_count"`
	VisitCount    int      `json:"visit_count"`
	Browsers      []string `json:"browsers"`
	Installed     bool     `json:"installed"`
	Description   string   `json:"description,omitempty"`
	RunCmd        string   `json:"run_cmd"`
}

// DefaultBrowserSources discovers local Ardia, Google Chrome, and Firefox-family
// profile paths on macOS and Linux. Filter may be "", "all", "ardia", "chrome",
// or "firefox".
func DefaultBrowserSources(filter string) []BrowserSource {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	f := strings.ToLower(strings.TrimSpace(filter))
	include := func(name string) bool {
		if f == "" || f == "all" {
			return true
		}
		return f == name
	}

	var sources []BrowserSource
	switch runtime.GOOS {
	case "darwin":
		appSup := filepath.Join(home, "Library", "Application Support")
		if include("ardia") {
			base := filepath.Join(appSup, "com.ardia.browser", "Chromium", "Default")
			sources = append(sources, BrowserSource{
				Name:        "ardia",
				Engine:      EngineChromium,
				CookiesPath: filepath.Join(base, "Cookies"),
				HistoryPath: filepath.Join(base, "History"),
			})
		}
		if include("chrome") {
			base := filepath.Join(appSup, "Google", "Chrome", "Default")
			sources = append(sources, BrowserSource{
				Name:        "chrome",
				Engine:      EngineChromium,
				CookiesPath: filepath.Join(base, "Cookies"),
				HistoryPath: filepath.Join(base, "History"),
			})
		}
		if include("firefox") {
			for _, pair := range []struct{ label, dir string }{
				{"zen", filepath.Join(appSup, "zen", "Profiles")},
				{"firefox", filepath.Join(appSup, "Firefox", "Profiles")},
				{"librewolf", filepath.Join(appSup, "LibreWolf", "Profiles")},
			} {
				sources = append(sources, discoverFirefoxProfiles(pair.label, pair.dir)...)
			}
		}
	case "linux":
		if include("chrome") {
			base := filepath.Join(home, ".config", "google-chrome", "Default")
			sources = append(sources, BrowserSource{
				Name:        "chrome",
				Engine:      EngineChromium,
				CookiesPath: filepath.Join(base, "Cookies"),
				HistoryPath: filepath.Join(base, "History"),
			})
		}
		if include("firefox") {
			for _, pair := range []struct{ label, dir string }{
				{"zen", filepath.Join(home, ".zen")},
				{"firefox", filepath.Join(home, ".mozilla", "firefox")},
				{"librewolf", filepath.Join(home, ".librewolf")},
			} {
				sources = append(sources, discoverFirefoxProfiles(pair.label, pair.dir)...)
			}
		}
	}
	return sources
}

func discoverFirefoxProfiles(label, root string) []BrowserSource {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []BrowserSource
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		cookies := filepath.Join(dir, "cookies.sqlite")
		history := filepath.Join(dir, "places.sqlite")
		if fileExists(cookies) || fileExists(history) {
			out = append(out, BrowserSource{
				Name:        label,
				Engine:      EngineFirefox,
				CookiesPath: cookies,
				HistoryPath: history,
			})
		}
	}
	return out
}

// ScanLocalDomains reads ONLY plaintext domain names, non-expired cookie counts,
// and visit counts from WAL-safe temporary copies of the supplied browser DBs.
// Cookie values, encrypted_value blobs, and URL paths are never selected or kept.
func ScanLocalDomains(sources []BrowserSource) ([]DomainSignal, error) {
	type agg struct {
		cookieCount int
		visitCount  int
		browsers    map[string]struct{}
	}
	byDomain := make(map[string]*agg)
	addSignal := func(rawHost, browser string, cookies, visits int) {
		dom := normalizeDomain(rawHost)
		if dom == "" {
			return
		}
		entry, ok := byDomain[dom]
		if !ok {
			entry = &agg{browsers: make(map[string]struct{})}
			byDomain[dom] = entry
		}
		entry.cookieCount += cookies
		entry.visitCount += visits
		if browser != "" {
			entry.browsers[browser] = struct{}{}
		}
	}

	nowUnix := time.Now().Unix()
	// Chromium epoch is 1601-01-01 in microseconds.
	chromiumNowMicros := (nowUnix + 11644473600) * 1000000

	for _, src := range sources {
		switch src.Engine {
		case EngineChromium:
			if fileExists(src.CookiesPath) {
				_ = scanChromiumCookies(src.CookiesPath, chromiumNowMicros, func(host string, cnt int) {
					addSignal(host, src.Name, cnt, 0)
				})
			}
			if fileExists(src.HistoryPath) {
				_ = scanChromiumHistory(src.HistoryPath, func(host string, visits int) {
					addSignal(host, src.Name, 0, visits)
				})
			}
		case EngineFirefox:
			if fileExists(src.CookiesPath) {
				_ = scanFirefoxCookies(src.CookiesPath, nowUnix, func(host string, cnt int) {
					addSignal(host, src.Name, cnt, 0)
				})
			}
			if fileExists(src.HistoryPath) {
				_ = scanFirefoxHistory(src.HistoryPath, func(host string, visits int) {
					addSignal(host, src.Name, 0, visits)
				})
			}
		}
	}

	out := make([]DomainSignal, 0, len(byDomain))
	for dom, a := range byDomain {
		browsers := make([]string, 0, len(a.browsers))
		for b := range a.browsers {
			browsers = append(browsers, b)
		}
		sort.Strings(browsers)
		out = append(out, DomainSignal{
			Domain:      dom,
			HasSession:  a.cookieCount > 0,
			CookieCount: a.cookieCount,
			VisitCount:  a.visitCount,
			Browsers:    browsers,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HasSession != out[j].HasSession {
			return out[i].HasSession
		}
		if out[i].VisitCount != out[j].VisitCount {
			return out[i].VisitCount > out[j].VisitCount
		}
		if out[i].CookieCount != out[j].CookieCount {
			return out[i].CookieCount > out[j].CookieCount
		}
		return out[i].Domain < out[j].Domain
	})
	return out, nil
}

// wellKnownDomainAliases bridges common web domains whose homepage differs from
// the exact slug or subdomain used in printing-press-library.
var wellKnownDomainAliases = map[string]string{
	"monarchmoney.com":     "monarch-money",
	"app.monarchmoney.com": "monarch-money",
	"mail.google.com":      "gmail",
	"calendar.google.com":  "google-calendar",
	"x.com":                "x-twitter",
	"twitter.com":          "x-twitter",
	"news.ycombinator.com": "hackernews",
	"amazon.com":           "amazon-orders",
	"www.amazon.com":       "amazon-orders",
	"sameday.costco.com":   "costco-sameday",
	"github.com":           "github",
	"linear.app":           "linear",
	"notion.so":            "notion",
}

// MatchCatalog cross-references local browser DomainSignals against installed
// Cartridges, embedded starters, and the 616-app Printing Press Library.
func MatchCatalog(signals []DomainSignal, cat *registry.Catalog, installedCarts, builtinCarts []*cartridge.Cartridge, limit int) []AppRecommendation {
	if limit <= 0 {
		limit = 25
	}

	catalogByName := make(map[string]registry.Entry)
	catalogByHost := make(map[string]registry.Entry)
	if cat != nil {
		for _, e := range cat.Entries {
			if e.Name == "chrome-history" {
				// chrome-history is a local browser-history tool, not a google.com web app.
				continue
			}
			catalogByName[e.Name] = e
			if e.Homepage != "" {
				if h := normalizeDomain(e.Homepage); h != "" {
					if _, exists := catalogByHost[h]; !exists {
						catalogByHost[h] = e
					}
				}
			}
		}
	}

	installedCartByName := make(map[string]*cartridge.Cartridge, len(installedCarts))
	for _, c := range installedCarts {
		installedCartByName[c.Name] = c
	}
	builtinCartByName := make(map[string]*cartridge.Cartridge, len(builtinCarts))
	for _, c := range builtinCarts {
		builtinCartByName[c.Name] = c
	}

	recsByApp := make(map[string]*AppRecommendation)
	mergeRec := func(rec AppRecommendation) {
		existing, ok := recsByApp[rec.App]
		if !ok {
			cp := rec
			recsByApp[rec.App] = &cp
			return
		}
		existing.HasSession = existing.HasSession || rec.HasSession
		existing.CookieCount += rec.CookieCount
		existing.VisitCount += rec.VisitCount
		bset := make(map[string]struct{}, len(existing.Browsers)+len(rec.Browsers))
		for _, b := range existing.Browsers {
			bset[b] = struct{}{}
		}
		for _, b := range rec.Browsers {
			bset[b] = struct{}{}
		}
		mergedBrowsers := make([]string, 0, len(bset))
		for b := range bset {
			mergedBrowsers = append(mergedBrowsers, b)
		}
		sort.Strings(mergedBrowsers)
		existing.Browsers = mergedBrowsers
	}

	for _, sig := range signals {
		// 1. Check installed & built-in Cartridges first.
		matchedCart := false
		for _, c := range installedCarts {
			if c.MatchDomain(sig.Domain) {
				mergeRec(AppRecommendation{
					App:           c.Name,
					Kind:          "cartridge",
					Category:      c.Category,
					MatchedDomain: sig.Domain,
					HasSession:    sig.HasSession,
					CookieCount:   sig.CookieCount,
					VisitCount:    sig.VisitCount,
					Browsers:      sig.Browsers,
					Installed:     true,
					Description:   c.Description,
					RunCmd:        "pp run " + c.Name + " --help",
				})
				matchedCart = true
				break
			}
		}
		if matchedCart {
			continue
		}
		for _, c := range builtinCarts {
			if c.MatchDomain(sig.Domain) {
				mergeRec(AppRecommendation{
					App:           c.Name,
					Kind:          "cartridge",
					Category:      c.Category,
					MatchedDomain: sig.Domain,
					HasSession:    sig.HasSession,
					CookieCount:   sig.CookieCount,
					VisitCount:    sig.VisitCount,
					Browsers:      sig.Browsers,
					Installed:     true,
					Description:   c.Description,
					RunCmd:        "pp run " + c.Name + " --help",
				})
				matchedCart = true
				break
			}
		}
		if matchedCart {
			continue
		}

		// 2. Match against well-known aliases or catalog homepage hosts.
		var matchedEntry *registry.Entry
		if alias, ok := wellKnownDomainAliases[sig.Domain]; ok {
			if e, ok := catalogByName[alias]; ok {
				matchedEntry = &e
			}
		}
		if matchedEntry == nil {
			for _, candidate := range domainSuffixes(sig.Domain) {
				if e, ok := catalogByHost[candidate]; ok {
					matchedEntry = &e
					break
				}
				if alias, ok := wellKnownDomainAliases[candidate]; ok {
					if e, ok := catalogByName[alias]; ok {
						matchedEntry = &e
						break
					}
				}
			}
		}
		if matchedEntry == nil {
			continue
		}

		_, binInstalled := registry.InstalledBinaryPath(matchedEntry.Name)
		_, isInstalledCart := installedCartByName[matchedEntry.Name]
		_, isBuiltinCart := builtinCartByName[matchedEntry.Name]
		mergeRec(AppRecommendation{
			App:           matchedEntry.Name,
			Kind:          "prebuilt-binary",
			Category:      matchedEntry.Category,
			MatchedDomain: sig.Domain,
			HasSession:    sig.HasSession,
			CookieCount:   sig.CookieCount,
			VisitCount:    sig.VisitCount,
			Browsers:      sig.Browsers,
			Installed:     binInstalled || isInstalledCart || isBuiltinCart,
			Description:   matchedEntry.Description,
			RunCmd:        "pp run " + matchedEntry.Name + " --help",
		})
	}

	out := make([]AppRecommendation, 0, len(recsByApp))
	for _, rec := range recsByApp {
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HasSession != out[j].HasSession {
			return out[i].HasSession
		}
		if out[i].VisitCount != out[j].VisitCount {
			return out[i].VisitCount > out[j].VisitCount
		}
		if out[i].CookieCount != out[j].CookieCount {
			return out[i].CookieCount > out[j].CookieCount
		}
		return out[i].App < out[j].App
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func scanChromiumCookies(srcPath string, nowMicros int64, emit func(host string, count int)) error {
	db, cleanup, err := openSnapshotReadOnly(srcPath)
	if err != nil {
		return err
	}
	defer cleanup()

	// Strictly query ONLY host_key and count - never select value or encrypted_value.
	rows, err := db.Query(`SELECT host_key, COUNT(*) FROM cookies WHERE expires_utc = 0 OR expires_utc > ? GROUP BY host_key`, nowMicros)
	if err != nil {
		rows, err = db.Query(`SELECT host_key, COUNT(*) FROM cookies GROUP BY host_key`)
		if err != nil {
			return err
		}
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var host string
		var cnt int
		if rows.Scan(&host, &cnt) == nil && cnt > 0 {
			emit(host, cnt)
		}
	}
	return rows.Err()
}

func scanChromiumHistory(srcPath string, emit func(host string, visits int)) error {
	db, cleanup, err := openSnapshotReadOnly(srcPath)
	if err != nil {
		return err
	}
	defer cleanup()

	rows, err := db.Query(`SELECT url, visit_count FROM urls WHERE visit_count > 0`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var rawURL string
		var visits int
		if rows.Scan(&rawURL, &visits) == nil && visits > 0 {
			emit(rawURL, visits)
		}
	}
	return rows.Err()
}

func scanFirefoxCookies(srcPath string, nowUnix int64, emit func(host string, count int)) error {
	db, cleanup, err := openSnapshotReadOnly(srcPath)
	if err != nil {
		return err
	}
	defer cleanup()

	// Strictly query ONLY host and count - never select value.
	rows, err := db.Query(`SELECT host, COUNT(*) FROM moz_cookies WHERE expiry = 0 OR expiry > ? GROUP BY host`, nowUnix)
	if err != nil {
		rows, err = db.Query(`SELECT host, COUNT(*) FROM moz_cookies GROUP BY host`)
		if err != nil {
			return err
		}
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var host string
		var cnt int
		if rows.Scan(&host, &cnt) == nil && cnt > 0 {
			emit(host, cnt)
		}
	}
	return rows.Err()
}

func scanFirefoxHistory(srcPath string, emit func(host string, visits int)) error {
	db, cleanup, err := openSnapshotReadOnly(srcPath)
	if err != nil {
		return err
	}
	defer cleanup()

	rows, err := db.Query(`SELECT url, visit_count FROM moz_places WHERE visit_count > 0`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var rawURL string
		var visits int
		if rows.Scan(&rawURL, &visits) == nil && visits > 0 {
			emit(rawURL, visits)
		}
	}
	return rows.Err()
}

func openSnapshotReadOnly(srcPath string) (*sql.DB, func(), error) {
	tmpDir, err := os.MkdirTemp("", "pp-onboard-snap-*")
	if err != nil {
		return nil, nil, err
	}
	dst := filepath.Join(tmpDir, "snap.db")
	if err := copyFile(srcPath, dst); err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = copyFile(srcPath+suffix, dst+suffix)
	}
	db, err := sql.Open("sqlite", dst+"?mode=ro")
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, nil, err
	}
	_, _ = db.Exec("PRAGMA query_only = ON")
	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return db, cleanup, nil
}

func normalizeDomain(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return ""
		}
		s = strings.ToLower(u.Hostname())
	} else {
		if idx := strings.IndexByte(s, '/'); idx >= 0 {
			s = s[:idx]
		}
		if idx := strings.IndexByte(s, ':'); idx >= 0 {
			s = s[:idx]
		}
	}
	s = strings.TrimPrefix(s, ".")
	s = strings.TrimPrefix(s, "www.")
	if s == "" || !strings.Contains(s, ".") {
		return ""
	}
	return s
}

func domainSuffixes(dom string) []string {
	parts := strings.Split(dom, ".")
	if len(parts) < 2 {
		return []string{dom}
	}
	out := make([]string, 0, len(parts)-1)
	for i := 0; i <= len(parts)-2; i++ {
		out = append(out, strings.Join(parts[i:], "."))
	}
	return out
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
