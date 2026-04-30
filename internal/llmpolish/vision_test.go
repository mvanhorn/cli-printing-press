package llmpolish

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSynthesizeVisionReturnsNilWhenLLMUnavailable(t *testing.T) {
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	defer os.Setenv("PATH", originalPath)

	customization, err := SynthesizeVision(nil, nil)

	require.NoError(t, err)
	assert.Nil(t, customization)
}

func TestParseVisionCustomization(t *testing.T) {
	raw := `{"resource_priority":["messages","members","channels"],"fts_fields":{"messages":["content"],"members":["username","bio"]},"workflow_names":{"archive":"archive","audit":"audit-log"},"example_overrides":{"sync":"discord-pp-cli sync --guild 1234567890"},"desc_overrides":{"sync":"Sync guild messages to local SQLite for offline search"},"sync_hints":{"messages":{"direction":"newest_first","batch_size":100,"priority":1}}}`

	var vc VisionCustomization
	err := json.Unmarshal([]byte(raw), &vc)

	require.NoError(t, err)
	assert.Len(t, vc.ResourcePriority, 3)
	assert.Equal(t, []string{"content"}, vc.FTSFields["messages"])
	assert.Equal(t, "archive", vc.WorkflowNames["archive"])
}

func TestBuildVisionPrompt(t *testing.T) {
	profile := &profiler.APIProfile{
		HighVolume:  true,
		NeedsSearch: true,
		SyncableResources: []profiler.SyncableResource{
			{Name: "messages", Path: "/channels/{channel_id}/messages"},
			{Name: "channels", Path: "/guilds/{guild_id}/channels"},
		},
		SearchableFields: map[string][]string{
			"messages": {"content", "author"},
		},
	}
	apiSpec := &spec.APISpec{
		Name:        "discord",
		Description: "Discord API",
		Resources: map[string]spec.Resource{
			"channels": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/channels"},
				},
			},
		},
	}

	prompt := buildVisionPrompt(profile, apiSpec)

	assert.Contains(t, prompt, "discord")
	assert.Contains(t, prompt, "messages")
	assert.Contains(t, prompt, "High volume: true")
}

func TestBuildVisionPromptNilSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		prompt := buildVisionPrompt(nil, nil)
		assert.Contains(t, prompt, "unknown")
		assert.Contains(t, prompt, "High volume: false")
		assert.Contains(t, prompt, "Resources and their endpoints:")
	})
}
