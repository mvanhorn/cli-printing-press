package mcpdesc

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
)

func TestParamDescriptionCompactorPassesThroughUniqueAndShortDescriptions(t *testing.T) {
	uniqueDescription := "Unique endpoint-specific filter text."
	api := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Params: []spec.Param{
							{Name: "expand", Type: "string", Description: uniqueDescription},
							{Name: "limit", Type: "integer", Description: "Max items to return"},
						},
					},
				},
			},
		},
	}

	compactor := NewParamDescriptionCompactor(api)

	assert.Equal(t, uniqueDescription, compactor.Description(spec.Param{Name: "expand", Type: "string", Description: uniqueDescription}))
	assert.Equal(t, "Max items to return", compactor.Description(spec.Param{Name: "limit", Type: "integer", Description: "Max items to return"}))
}

func TestParamDescriptionCompactorUsesFullDescriptionsForKeysAndPassThrough(t *testing.T) {
	sharedPrefix := strings.Repeat("Shared endpoint expansion guidance ", 5)
	ownerDescription := sharedPrefix + "Allowed values: owner, creator, updater, and permissionSummary."
	statusDescription := sharedPrefix + "Allowed values: status, lifecycle, archived, and moderationState."
	auditDescription := sharedPrefix + "Allowed values: createdAt, updatedAt, actor, and requestId."
	api := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Params: []spec.Param{{Name: "expand", Type: "string", Description: ownerDescription}},
					},
					"search": {
						Params: []spec.Param{{Name: "expand", Type: "string", Description: statusDescription}},
					},
					"recent": {
						Params: []spec.Param{{Name: "expand", Type: "string", Description: auditDescription}},
					},
				},
			},
		},
	}

	compactor := NewParamDescriptionCompactor(api)

	assert.Equal(t, naming.OneLineNormalize(ownerDescription), compactor.Description(spec.Param{Name: "expand", Type: "string", Description: ownerDescription}))
	assert.Equal(t, naming.OneLineNormalize(statusDescription), compactor.Description(spec.Param{Name: "expand", Type: "string", Description: statusDescription}))
	assert.Equal(t, naming.OneLineNormalize(auditDescription), compactor.Description(spec.Param{Name: "expand", Type: "string", Description: auditDescription}))
}

func TestParamDescriptionCompactorNormalizesEmptyStringTypes(t *testing.T) {
	description := "Select additional nested resource fields to include in the response. Use comma-separated field names such as owner, permissions, metadata, relationships, and auditTrail; unsupported values are ignored by the upstream API."
	api := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{
					"list":   {Params: []spec.Param{{Name: "expand", Description: description}}},
					"search": {Params: []spec.Param{{Name: "expand", Type: "string", Description: description}}},
					"recent": {Params: []spec.Param{{Name: "expand", Description: description}}},
				},
			},
		},
	}

	compactor := NewParamDescriptionCompactor(api)

	assert.Equal(t,
		"Select additional nested resource fields to include in the response.",
		compactor.Description(spec.Param{Name: "expand", Type: "string", Description: description}),
	)
}

func TestParamDescriptionCompactorTruncatesUnicodeSafely(t *testing.T) {
	description := strings.Repeat("cafe\u0301 metadata ", 16)
	api := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{
					"list":   {Params: []spec.Param{{Name: "expand", Type: "string", Description: description}}},
					"search": {Params: []spec.Param{{Name: "expand", Type: "string", Description: description}}},
					"recent": {Params: []spec.Param{{Name: "expand", Type: "string", Description: description}}},
				},
			},
		},
	}

	got := NewParamDescriptionCompactor(api).Description(spec.Param{Name: "expand", Type: "string", Description: description})

	assert.True(t, utf8.ValidString(got))
	assert.LessOrEqual(t, utf8.RuneCountInString(got), sharedParamDescriptionMax)
	assert.True(t, strings.HasSuffix(got, sharedParamDescriptionTail))
}
