package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
)

type ApifyActorAuditVerdict string

const (
	ApifyActorAuditPass       ApifyActorAuditVerdict = "pass"
	ApifyActorAuditFail       ApifyActorAuditVerdict = "fail"
	ApifyActorAuditUnverified ApifyActorAuditVerdict = "unverified"
)

type ApifyActorAuditStatus string

const (
	ApifyActorStatusReachable  ApifyActorAuditStatus = "reachable"
	ApifyActorStatusMissing    ApifyActorAuditStatus = "missing"
	ApifyActorStatusAuth       ApifyActorAuditStatus = "auth-required"
	ApifyActorStatusUnverified ApifyActorAuditStatus = "unverified"
)

type ApifyActorAuditOptions struct {
	Dir         string
	ResearchDir string
	BaseURL     string
	Token       string
	HTTPClient  *http.Client
}

type ApifyActorAuditActor struct {
	ID      string                `json:"id"`
	Status  ApifyActorAuditStatus `json:"status"`
	Sources []string              `json:"sources"`
	Detail  string                `json:"detail,omitempty"`
}

type ApifyActorAuditReport struct {
	Dir         string                 `json:"dir"`
	ResearchDir string                 `json:"research_dir,omitempty"`
	Verdict     ApifyActorAuditVerdict `json:"verdict"`
	Actors      []ApifyActorAuditActor `json:"actors"`
	Issues      []string               `json:"issues,omitempty"`
}

var apifyActPathRe = regexp.MustCompile(`/acts/([A-Za-z0-9][A-Za-z0-9_-]*~[A-Za-z0-9][A-Za-z0-9_-]*)(?:/|%2[Ff]|[?#"'\s),;\]}]|$)`)

func RunApifyActorAudit(ctx context.Context, opts ApifyActorAuditOptions) (*ApifyActorAuditReport, error) {
	if strings.TrimSpace(opts.Dir) == "" {
		return nil, fmt.Errorf("--dir is required")
	}
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.apify.com"
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		token = apifyTokenFromEnv()
	}

	sources, err := scanApifyActorReferences(opts.Dir, opts.ResearchDir)
	if err != nil {
		return nil, err
	}

	report := &ApifyActorAuditReport{
		Dir:         opts.Dir,
		ResearchDir: opts.ResearchDir,
		Verdict:     ApifyActorAuditPass,
	}
	if len(sources) == 0 {
		report.Issues = []string{"no Apify actor references found, skipping"}
		return report, nil
	}

	ids := make([]string, 0, len(sources))
	for id := range sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		actor := ApifyActorAuditActor{
			ID:      id,
			Sources: sources[id],
		}
		status, detail := probeApifyActor(ctx, client, baseURL, token, id)
		actor.Status = status
		actor.Detail = detail
		report.Actors = append(report.Actors, actor)

		switch status {
		case ApifyActorStatusMissing:
			report.Verdict = ApifyActorAuditFail
			report.Issues = append(report.Issues, fmt.Sprintf("Apify actor %q is missing (GET /v2/acts/%s returned 404); referenced by %s", id, id, strings.Join(actor.Sources, ", ")))
		case ApifyActorStatusAuth, ApifyActorStatusUnverified:
			if report.Verdict != ApifyActorAuditFail {
				report.Verdict = ApifyActorAuditUnverified
			}
			report.Issues = append(report.Issues, fmt.Sprintf("Apify actor %q could not be verified: %s; referenced by %s", id, detail, strings.Join(actor.Sources, ", ")))
		}
	}

	return report, nil
}

func WriteApifyActorAuditReport(dir string, report *ApifyActorAuditReport) error {
	emitted := *report
	emitted.Dir = artifacts.RedactCLIDirRoot(report.Dir)
	if report.ResearchDir != "" {
		emitted.ResearchDir = artifacts.RedactCLIDirRoot(report.ResearchDir)
	}
	data, err := json.MarshalIndent(&emitted, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling Apify actor audit report: %w", err)
	}
	path := filepath.Join(dir, "apify-actor-audit-report.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing Apify actor audit report: %w", err)
	}
	return nil
}

func scanApifyActorReferences(dir, researchDir string) (map[string][]string, error) {
	roots := []string{dir}
	if researchDir != "" && researchDir != dir {
		roots = append(roots, researchDir)
	}
	found := make(map[string]map[string]struct{})
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if shouldSkipApifyAuditDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !shouldScanApifyAuditFile(path) {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, id := range apifyActorIDsInContent(string(data)) {
				if found[id] == nil {
					found[id] = make(map[string]struct{})
				}
				found[id][path] = struct{}{}
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("scanning Apify actor references under %s: %w", root, err)
		}
	}

	out := make(map[string][]string, len(found))
	for id, sourceSet := range found {
		for source := range sourceSet {
			out[id] = append(out[id], source)
		}
		sort.Strings(out[id])
	}
	return out, nil
}

func shouldSkipApifyAuditDir(name string) bool {
	switch name {
	case ".git", ".gotmp", "node_modules", "vendor", "dist", "build":
		return true
	default:
		return false
	}
}

func shouldScanApifyAuditFile(path string) bool {
	if filepath.Base(path) == "apify-actor-audit-report.json" {
		return false
	}
	switch filepath.Ext(path) {
	case ".go", ".json", ".md", ".yaml", ".yml", ".txt":
		return true
	default:
		return false
	}
}

func apifyActorIDsInContent(content string) []string {
	if !strings.Contains(content, "/acts/") {
		return nil
	}
	seen := make(map[string]struct{})
	for _, m := range apifyActPathRe.FindAllStringSubmatch(content, -1) {
		seen[m[1]] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func probeApifyActor(ctx context.Context, client *http.Client, baseURL, token, actorID string) (ApifyActorAuditStatus, string) {
	endpoint := fmt.Sprintf("%s/v2/acts/%s", baseURL, url.PathEscape(actorID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ApifyActorStatusUnverified, err.Error()
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return ApifyActorStatusUnverified, err.Error()
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		return ApifyActorStatusReachable, "reachable"
	case http.StatusNotFound:
		return ApifyActorStatusMissing, "not found"
	case http.StatusUnauthorized, http.StatusForbidden:
		if token == "" {
			return ApifyActorStatusAuth, fmt.Sprintf("GET /v2/acts/%s returned %d; set APIFY_TOKEN to verify private actors", actorID, resp.StatusCode)
		}
		return ApifyActorStatusAuth, fmt.Sprintf("GET /v2/acts/%s returned %d with configured Apify token", actorID, resp.StatusCode)
	default:
		return ApifyActorStatusUnverified, fmt.Sprintf("GET /v2/acts/%s returned %d", actorID, resp.StatusCode)
	}
}

func apifyTokenFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("APIFY_TOKEN")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("APIFY_API_TOKEN"))
}
