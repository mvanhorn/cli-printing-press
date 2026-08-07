package ble

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactSensitiveTerms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		terms []string
		want  string
	}{
		{
			name:  "possessive form replaced without residual apostrophe-s",
			value: "Owner's Device",
			terms: []string{"Owner"},
			want:  "redacted Device",
		},
		{
			name:  "bare term replaced",
			value: "Owner Device",
			terms: []string{"Owner"},
			want:  "redacted Device",
		},
		{
			name:  "empty term is no-op",
			value: "Owner's Device",
			terms: []string{""},
			want:  "Owner's Device",
		},
		{
			name:  "whitespace-only term is no-op",
			value: "Owner's Device",
			terms: []string{"   "},
			want:  "Owner's Device",
		},
		{
			name:  "nil terms slice is no-op",
			value: "Owner's Device",
			terms: nil,
			want:  "Owner's Device",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := redactSensitiveTerms(tc.value, buildRedactionPatterns(tc.terms))
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRedactEvidenceRedactsDeviceNameAndAnonymizesAddress(t *testing.T) {
	t.Parallel()

	input := EvidenceInput{
		RedactionTerms: []string{"Owner"},
		Events: []Event{
			{ID: "adv-1", Type: EventAdvertisement, DeviceAddress: "AA:BB:CC:DD:EE:01", DeviceName: "Owner's Speaker"},
			{ID: "adv-2", Type: EventAdvertisement, DeviceAddress: "AA:BB:CC:DD:EE:01", DeviceName: "Owner Device"},
		},
	}

	result := RedactEvidence(input)

	assert.Equal(t, "redacted Speaker", result.Events[0].DeviceName)
	assert.Equal(t, "device-1", result.Events[0].DeviceAddress)

	assert.Equal(t, "redacted Device", result.Events[1].DeviceName)
	assert.Equal(t, "device-1", result.Events[1].DeviceAddress)
}

func TestRedactEvidenceRedactsDisplayName(t *testing.T) {
	t.Parallel()

	input := EvidenceInput{
		Name:           "owner-gadget",
		DisplayName:    "Owner's Gadget",
		RedactionTerms: []string{"Owner"},
	}

	result := RedactEvidence(input)

	assert.Equal(t, "redacted Gadget", result.DisplayName)
}
