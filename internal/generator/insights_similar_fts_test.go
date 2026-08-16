package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestGenerateInsightsSimilarSanitizesSourceTitleForFTSMatch(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("similar-fts-sanitize")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Store:    true,
		Insights: []string{"insights/similar.go.tmpl"},
	}
	require.NoError(t, gen.Generate())

	similarSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "similar.go"))
	require.NoError(t, err)
	require.Contains(t, string(similarSrc), "matchQuery := store.FTSMatchQuery(searchText)")
	require.Contains(t, string(similarSrc), "db.DB().Query(query, matchQuery, itemID, sourceType, limit)")

	inlineTest := `package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

func TestSimilarSanitizesSourceTitleForFTSMatch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	tests := []struct {
		name        string
		sourceID    string
		peerID      string
		title       string
		peerTitle   string
		wantSimilar bool
	}{
		{
			name:        "comma and parens",
			sourceID:    "comma-source",
			peerID:      "comma-peer",
			title:       "Well drilling, Phase 2 (residential)",
			peerTitle:   "Well drilling Phase 2 residential followup",
			wantSimilar: true,
		},
		{
			name:        "embedded quote",
			sourceID:    "quote-source",
			peerID:      "quote-peer",
			title:       "Alpha \"quoted\" beta",
			peerTitle:   "Alpha quoted beta followup",
			wantSimilar: true,
		},
		{
			name:        "leading hyphen",
			sourceID:    "hyphen-source",
			peerID:      "hyphen-peer",
			title:       "-Draft- proposal",
			peerTitle:   "Draft proposal review",
			wantSimilar: true,
		},
		{
			name:        "bare operators",
			sourceID:    "operators-source",
			peerID:      "operators-peer",
			title:       "AND OR NOT",
			peerTitle:   "AND OR NOT checklist",
			wantSimilar: true,
		},
		{
			name:        "punctuation only",
			sourceID:    "punctuation-source",
			title:       "!!! , -- ()",
			wantSimilar: false,
		},
	}

	for _, tt := range tests {
		upsertSimilarRecord(t, db, tt.sourceID, tt.title)
		if tt.wantSimilar {
			upsertSimilarRecord(t, db, tt.peerID, tt.peerTitle)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runSimilarJSON(t, dbPath, tt.sourceID)
			if got.ResultCount != len(got.Similar) {
				t.Fatalf("result_count = %d, len(similar) = %d", got.ResultCount, len(got.Similar))
			}
			if !tt.wantSimilar {
				if len(got.Similar) != 0 {
					t.Fatalf("punctuation-only title returned similar rows: %+v", got.Similar)
				}
				return
			}
			if !similarContainsID(got.Similar, tt.peerID) {
				t.Fatalf("similar results missing %q: %+v", tt.peerID, got.Similar)
			}
		})
	}
}

type similarOutput struct {
	Similar []struct {
		ID string ` + "`" + `json:"id"` + "`" + `
	} ` + "`" + `json:"similar"` + "`" + `
	ResultCount int ` + "`" + `json:"result_count"` + "`" + `
}

func upsertSimilarRecord(t *testing.T, db *store.Store, id, title string) {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"id":    id,
		"title": title,
	})
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if err := db.Upsert("tickets", id, data); err != nil {
		t.Fatalf("upsert %s: %v", id, err)
	}
}

func runSimilarJSON(t *testing.T, dbPath, sourceID string) similarOutput {
	t.Helper()
	cmd := newSimilarCmd(&rootFlags{asJSON: true})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{sourceID, "--db", dbPath, "--type", "tickets", "--limit", "10"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("similar %s: %v", sourceID, err)
	}
	var got similarOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("parse similar output: %v\n%s", err, out.String())
	}
	return got
}

func similarContainsID(rows []struct {
	ID string ` + "`" + `json:"id"` + "`" + `
}, id string) bool {
	for _, row := range rows {
		if row.ID == id {
			return true
		}
	}
	return false
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "similar_fts_sanitize_test.go"), []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "mod", "tidy")
	runGoCommandRequired(t, outputDir, "test", "./internal/store", "-run", "^TestFTSMatchQuerySanitizesPunctuation$", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestSimilarSanitizesSourceTitleForFTSMatch$", "-count=1")
}
