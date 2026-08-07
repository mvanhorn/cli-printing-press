package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScoreREADMEAwardsTwoPointBonusForRecipesSection(t *testing.T) {
	t.Parallel()

	withoutRecipes := t.TempDir()
	writeScorecardFixture(t, withoutRecipes, "README.md", `# Recipe Score CLI

Review item status before follow-up work.

## Quick Start

`+"```bash"+`
recipe-score-pp-cli items list
`+"```"+`

## Agent Usage

Use --json and --agent for non-interactive operation.

## Health Check

`+"```bash"+`
recipe-score-pp-cli doctor
`+"```"+`

## Troubleshooting

Run doctor before retrying commands.
`)

	withRecipes := t.TempDir()
	writeScorecardFixture(t, withRecipes, "README.md", `# Recipe Score CLI

Review item status before follow-up work.

## Quick Start

`+"```bash"+`
recipe-score-pp-cli items list
`+"```"+`

## Agent Usage

Use --json and --agent for non-interactive operation.

## Health Check

`+"```bash"+`
recipe-score-pp-cli doctor
`+"```"+`

## Troubleshooting

Run doctor before retrying commands.

## Recipes

### Review stale items

`+"```bash"+`
recipe-score-pp-cli items list --json --select id,status
`+"```"+`

### Export item names

`+"```bash"+`
recipe-score-pp-cli items list --json --select id,name
`+"```"+`

### Inspect one item

`+"```bash"+`
recipe-score-pp-cli items get --id item_123 --json
`+"```"+`
`)

	assert.Equal(t, scoreREADME(withoutRecipes)+2, scoreREADME(withRecipes))
}
