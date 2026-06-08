package pipeline

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunApifyActorAuditDetectsGeneratedSourceAndResearchActors(t *testing.T) {
	cliDir := t.TempDir()
	researchDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(cliDir, "internal", "cli", "scrape.go"), `package cli

const tiktokRunsPath = "/v2/acts/apify~tiktok-scraper/runs"
`)
	writeTestFile(t, filepath.Join(researchDir, "research.json"), `{
  "notes": "Apify actors verified with GET /v2/acts/clockworks~tiktok-scraper/runs and GET /v2/acts/apify~instagram-scraper/runs"
}`)

	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/v2/acts/clockworks~tiktok-scraper", "/v2/acts/apify~instagram-scraper":
			w.WriteHeader(http.StatusOK)
		case "/v2/acts/apify~tiktok-scraper":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	report, err := RunApifyActorAudit(context.Background(), ApifyActorAuditOptions{
		Dir:         cliDir,
		ResearchDir: researchDir,
		BaseURL:     server.URL,
		Token:       "test-token",
	})
	require.NoError(t, err)

	assert.Equal(t, ApifyActorAuditFail, report.Verdict)
	require.Len(t, report.Actors, 3)
	assert.Equal(t, []string{
		"/v2/acts/apify~instagram-scraper",
		"/v2/acts/apify~tiktok-scraper",
		"/v2/acts/clockworks~tiktok-scraper",
	}, requested)
	assert.Contains(t, report.Issues[0], `Apify actor "apify~tiktok-scraper" is missing`)
	assert.Contains(t, report.Issues[0], "GET /v2/acts/apify~tiktok-scraper returned 404")
}

func TestRunApifyActorAuditPassesReachableActors(t *testing.T) {
	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(cliDir, "internal", "cli", "scrape.go"), `package cli

const instagramRunsPath = "https://api.apify.com/v2/acts/apify~instagram-scraper/runs"
`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/acts/apify~instagram-scraper", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	report, err := RunApifyActorAudit(context.Background(), ApifyActorAuditOptions{
		Dir:     cliDir,
		BaseURL: server.URL,
	})
	require.NoError(t, err)

	assert.Equal(t, ApifyActorAuditPass, report.Verdict)
	require.Len(t, report.Actors, 1)
	assert.Equal(t, ApifyActorStatusReachable, report.Actors[0].Status)
	assert.Empty(t, report.Issues)
}

func TestRunApifyActorAuditSkipsWhenNoActorReferences(t *testing.T) {
	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(cliDir, "internal", "cli", "regular.go"), `package cli

const ownerRepo = "someone~not-an-apify-actor"
`)

	report, err := RunApifyActorAudit(context.Background(), ApifyActorAuditOptions{Dir: cliDir})
	require.NoError(t, err)

	assert.Equal(t, ApifyActorAuditPass, report.Verdict)
	assert.Empty(t, report.Actors)
	require.Len(t, report.Issues, 1)
	assert.Contains(t, report.Issues[0], "no Apify actor references found")
}

func TestRunApifyActorAuditOnlyExtractsActorIDsFromActPaths(t *testing.T) {
	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "research"), 0o755))
	writeTestFile(t, filepath.Join(cliDir, "research", "research.json"), `{
  "timeRange": "2022~2024",
  "notes": "Fetched https://api.apify.com/v2/acts/apify~instagram-scraper and https://api.apify.com/v2/acts/apify~tiktok-scraper/runs"
}`)

	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/v2/acts/apify~instagram-scraper", "/v2/acts/apify~tiktok-scraper":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	report, err := RunApifyActorAudit(context.Background(), ApifyActorAuditOptions{
		Dir:     cliDir,
		BaseURL: server.URL,
	})
	require.NoError(t, err)

	assert.Equal(t, ApifyActorAuditPass, report.Verdict)
	assert.Equal(t, []string{
		"/v2/acts/apify~instagram-scraper",
		"/v2/acts/apify~tiktok-scraper",
	}, requested)
	require.Len(t, report.Actors, 2)
	assert.Equal(t, "apify~instagram-scraper", report.Actors[0].ID)
	assert.Equal(t, "apify~tiktok-scraper", report.Actors[1].ID)
}

func TestRunApifyActorAuditReportsAuthBlockedAsUnverified(t *testing.T) {
	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(cliDir, "internal", "cli", "scrape.go"), `package cli

const privateRunsPath = "https://api.apify.com/v2/acts/example~private-actor/runs"
`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	report, err := RunApifyActorAudit(context.Background(), ApifyActorAuditOptions{
		Dir:     cliDir,
		BaseURL: server.URL,
	})
	require.NoError(t, err)

	assert.Equal(t, ApifyActorAuditUnverified, report.Verdict)
	require.Len(t, report.Actors, 1)
	assert.Equal(t, ApifyActorStatusAuth, report.Actors[0].Status)
	require.Len(t, report.Issues, 1)
	assert.Contains(t, report.Issues[0], "set APIFY_TOKEN")
}
