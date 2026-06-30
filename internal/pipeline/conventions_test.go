package pipeline

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// conventionalCommitPattern validates the preferred format used in PR titles
// and commit messages. The PR title GitHub Action enforces conventional shape;
// this test keeps the repo's narrower documented scope list honest.
//
// Scopes: cli, skills, ci, plus chore(main) release PRs.
// Breaking changes: ! after scope.
var conventionalCommitPattern = regexp.MustCompile(
	`^(` +
		`(feat|fix|docs|chore|refactor|test|ci|perf|build|style|revert)` +
		`\((cli|skills|ci)\)` +
		`|chore\(main\)` +
		`)!?` +
		`: .+`)

func TestConventionalCommitPatternAcceptsValid(t *testing.T) {
	valid := []string{
		"feat(cli): add print subcommands",
		"fix(skills): remove repo checkout requirement",
		"feat(ci): add release-please",
		"chore(main): release 4.12.0",
		"feat(cli)!: rename catalog command to registry",
		"docs(cli): update version flag examples",
		"chore(ci): bump dependencies",
		"refactor(cli): extract helper function",
		"test(cli): add coverage for edge case",
		"fix(cli)!: breaking change with bang",
		"ci(ci): update workflow",
	}

	for _, msg := range valid {
		t.Run(msg, func(t *testing.T) {
			assert.Regexp(t, conventionalCommitPattern, msg)
		})
	}
}

func TestConventionalCommitPatternRejectsInvalid(t *testing.T) {
	invalid := []string{
		"Add new feature",
		"updated the readme",
		"WIP stuff",
		"FEAT(cli): wrong case",
		"feat:missing space",
		"feat(): empty scope",
		"feat: missing scope",
		"docs: missing scope",
		"feat(random): invalid scope",
		"feat(catalog): removed scope",
		"fix(anything): invalid scope",
		"feat(main): not a release",
	}

	for _, msg := range invalid {
		t.Run(msg, func(t *testing.T) {
			assert.NotRegexp(t, conventionalCommitPattern, msg)
		})
	}
}
