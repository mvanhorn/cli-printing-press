package piiplaceholders

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeCapturedExamples(t *testing.T) {
	description := "order 123-4567890-1234567, digital D01-4567890-1234567, ASIN B012345678, card ending in 4242"

	got := SanitizeCapturedExamples(description)

	assert.Contains(t, got, SyntheticOrderID)
	assert.Contains(t, got, SyntheticDigitalOrderID)
	assert.Contains(t, got, PrimarySyntheticASIN)
	assert.Contains(t, got, "card ending in "+SyntheticCardLast4)
	assert.NotContains(t, got, "123-4567890-1234567")
	assert.NotContains(t, got, "D01-4567890-1234567")
	assert.NotContains(t, got, "B012345678")
	assert.NotContains(t, got, "4242")
}
