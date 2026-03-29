package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// PressLearning records what happened during a single pipeline run.
type PressLearning struct {
	APIName  string          `json:"api_name"`
	Date     time.Time       `json:"date"`
	SpecType string          `json:"spec_type"`
	Issues   []LearningIssue `json:"issues"`
	Fixes    []LearningFix   `json:"fixes"`
}

// LearningIssue records a problem encountered during a pipeline phase.
type LearningIssue struct {
	Phase   string `json:"phase"`
	Gate    string `json:"gate"`
	Error   string `json:"error"`
	Pattern string `json:"pattern"`
}

// LearningFix records a solution applied for a known pattern.
type LearningFix struct {
	Pattern  string `json:"pattern"`
	Solution string `json:"solution"`
}

// LearningsDB is the on-disk store of all pipeline learnings.
type LearningsDB struct {
	Learnings []PressLearning `json:"learnings"`
}

// LearningsPath returns the path to the learnings database file.
func LearningsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".cache", "printing-press", "learnings.json")
	}
	return filepath.Join(home, ".cache", "printing-press", "learnings.json")
}

// LoadLearnings reads the learnings database from disk.
func LoadLearnings() (*LearningsDB, error) {
	path := LearningsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &LearningsDB{}, nil
		}
		return nil, err
	}
	var db LearningsDB
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, err
	}
	return &db, nil
}

// SaveLearning appends a learning to the database and writes to disk.
func SaveLearning(learning PressLearning) error {
	db, err := LoadLearnings()
	if err != nil {
		db = &LearningsDB{}
	}
	db.Learnings = append(db.Learnings, learning)

	path := LearningsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SuggestFlags returns CLI flag suggestions based on past learnings for
// similar spec types and sizes.
func SuggestFlags(specSize int, specType string) []string {
	db, err := LoadLearnings()
	if err != nil || len(db.Learnings) == 0 {
		return nil
	}

	// Collect patterns from past runs with the same spec type
	patternCounts := make(map[string]int)
	for _, l := range db.Learnings {
		if l.SpecType != specType {
			continue
		}
		for _, issue := range l.Issues {
			if issue.Pattern != "" {
				patternCounts[issue.Pattern]++
			}
		}
	}

	var flags []string
	if patternCounts["broken-ref"] > 0 {
		flags = append(flags, "--lenient")
	}
	if patternCounts["missing-auth"] > 0 {
		flags = append(flags, "--skip-auth-check")
	}
	if patternCounts["large-spec"] > 0 || specSize > 500000 {
		flags = append(flags, "--chunk-size=100")
	}
	if patternCounts["slow-generation"] > 0 {
		flags = append(flags, "--timeout=600")
	}
	if patternCounts["circular-ref"] > 0 {
		flags = append(flags, "--max-depth=5")
	}

	return flags
}
