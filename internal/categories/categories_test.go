package categories

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublicReturnsSortedPublicLibraryCategories(t *testing.T) {
	got := Public()

	assert.Equal(t, []string{
		"ai",
		"auth",
		"cloud",
		"commerce",
		"developer-tools",
		"devices",
		"food-and-dining",
		"health",
		"maps",
		"marketing",
		"media-and-entertainment",
		"monitoring",
		"other",
		"payments",
		"productivity",
		"project-management",
		"sales-and-crm",
		"social-and-messaging",
		"travel",
	}, got)
}

func TestIsPublic(t *testing.T) {
	for _, category := range Public() {
		assert.Truef(t, IsPublic(category), "IsPublic(%q)", category)
	}
	assert.False(t, IsPublic("internal-fixture"))
	assert.False(t, IsPublic("not-a-category"))
}
