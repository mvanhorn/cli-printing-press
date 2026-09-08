package generator

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// validateLearnTickerPlaybookReachability fails generate when ticker
// patterns would empty QueryFamily for seeded recall examples or for
// authored playbook JSON already in the output tree (reprint).
func validateLearnTickerPlaybookReachability(learn spec.LearnConfig, outputDir string) error {
	extras, err := authoredLearnQueryFamilyExamples(outputDir)
	if err != nil {
		return err
	}
	return spec.CheckLearnQueryFamilyReachability(&learn, extras)
}

func authoredLearnQueryFamilyExamples(outputDir string) ([]spec.LearnQueryFamilyExample, error) {
	matches, err := filepath.Glob(filepath.Join(outputDir, "internal", "cli", "playbooks", "*.json"))
	if err != nil {
		return nil, fmt.Errorf("listing authored playbooks: %w", err)
	}
	var extras []spec.LearnQueryFamilyExample
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading authored playbook %s: %w", path, err)
		}
		examples, err := spec.ParsePlaybookQueryFamilyExamples(data)
		if err != nil {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("playbooks", filepath.Base(path)))
		for _, query := range examples {
			extras = append(extras, spec.LearnQueryFamilyExample{Source: rel, Query: query})
		}
	}
	return extras, nil
}
