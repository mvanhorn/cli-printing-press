package pipeline

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// conventionalCommitPattern validates the format used in PR titles and
// commit messages. This pattern is mirrored by the PR title GitHub Action
// (.github/workflows/pr-title.yml) — keep them in sync.
var conventionalCommitPattern = regexp.MustCompile(
	`^(feat|fix|docs|chore|refactor|test|ci|perf|build|style|revert)` +
		`(\([a-z][a-z0-9-]*\))?` +
		`!?` +
		`: .+`)

func TestConventionalCommitPatternAcceptsValid(t *testing.T) {
	valid := []string{
		"feat(cli): add catalog subcommands",
		"fix(skills): remove repo checkout requirement",
		"feat(ci): add release-please",
		"feat(cli)!: rename catalog command to registry",
		"docs: update versioning guidance",
		"chore: bump dependencies",
		"refactor: extract helper function",
		"test: add coverage for edge case",
		"fix(cli)!: breaking change with bang",
		"ci: update workflow",
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
	}

	for _, msg := range invalid {
		t.Run(msg, func(t *testing.T) {
			assert.NotRegexp(t, conventionalCommitPattern, msg)
		})
	}
}
