package categories

import (
	"maps"
	"slices"
)

var valid = map[string]struct{}{
	"ai":                      {},
	"auth":                    {},
	"cloud":                   {},
	"commerce":                {},
	"developer-tools":         {},
	"devices":                 {},
	"food-and-dining":         {},
	"health":                  {},
	"maps":                    {},
	"marketing":               {},
	"media-and-entertainment": {},
	"monitoring":              {},
	"payments":                {},
	"productivity":            {},
	"project-management":      {},
	"sales-and-crm":           {},
	"social-and-messaging":    {},
	"travel":                  {},
	"other":                   {},
}

func Public() []string {
	return slices.Sorted(maps.Keys(valid))
}

func IsPublic(category string) bool {
	_, ok := valid[category]
	return ok
}
