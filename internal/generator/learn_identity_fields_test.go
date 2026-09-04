package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/stretchr/testify/require"
)

func TestLearnResourceIdentityFieldEntries_PrependsIDField(t *testing.T) {
	t.Parallel()

	api := minimalSpec("identity-fields")
	items := api.Resources["items"]
	list := items.Endpoints["list"]
	list.IDField = "sku"
	items.Endpoints["list"] = list
	api.Resources["items"] = items

	entries := learnResourceIdentityFieldEntries(api, profiler.Profile(api))
	require.NotEmpty(t, entries)

	var itemsEntry *learnIdentityFieldEntry
	for i := range entries {
		if entries[i].ResourceType == "items" {
			itemsEntry = &entries[i]
			break
		}
	}
	require.NotNil(t, itemsEntry, "items must appear in the identity-field map")
	require.NotEmpty(t, itemsEntry.Fields)
	require.Equal(t, "sku", itemsEntry.Fields[0], "resolved IDField must lead the identity list")
	require.Contains(t, itemsEntry.Fields, "name")
	require.Contains(t, itemsEntry.Fields, "id")
}

func TestLearnResourceIdentityFieldEntries_DedupesIDField(t *testing.T) {
	t.Parallel()

	api := minimalSpec("identity-dedupe")
	items := api.Resources["items"]
	list := items.Endpoints["list"]
	list.IDField = "id"
	items.Endpoints["list"] = list
	api.Resources["items"] = items

	entries := learnResourceIdentityFieldEntries(api, profiler.Profile(api))
	var itemsEntry learnIdentityFieldEntry
	for _, e := range entries {
		if e.ResourceType == "items" {
			itemsEntry = e
			break
		}
	}
	count := 0
	for _, f := range itemsEntry.Fields {
		if f == "id" {
			count++
		}
	}
	require.Equal(t, 1, count, "id must appear once even when it is both IDField and a common key")
}

func TestLearnResourceIdentityFieldEntries_SplitsCompositeIDField(t *testing.T) {
	t.Parallel()

	api := minimalSpec("identity-composite")
	items := api.Resources["items"]
	list := items.Endpoints["list"]
	list.IDField = "date+model_permaslug"
	items.Endpoints["list"] = list
	api.Resources["items"] = items

	entries := learnResourceIdentityFieldEntries(api, profiler.Profile(api))
	var itemsEntry *learnIdentityFieldEntry
	for i := range entries {
		if entries[i].ResourceType == "items" {
			itemsEntry = &entries[i]
			break
		}
	}
	require.NotNil(t, itemsEntry, "items must appear in the identity-field map")
	require.Equal(t, "date", itemsEntry.Fields[0], "composite IDField must split so the first part leads")
	require.Contains(t, itemsEntry.Fields, "model_permaslug")
	require.NotContains(t, itemsEntry.Fields, "date+model_permaslug")
}
