package generator

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/piiplaceholders"
)

// syntheticExampleValue returns canonical fake values for high-risk browser-
// sniff shapes so generated examples do not echo captured customer data.
func syntheticExampleValue(paramName string) (string, bool) {
	nameLower := strings.ToLower(paramName)
	isIDField := nameLower == "id" || strings.HasSuffix(nameLower, "_id") ||
		strings.HasSuffix(nameLower, "id")
	switch {
	case strings.Contains(nameLower, "asin"):
		return piiplaceholders.PrimarySyntheticASIN, true
	case (strings.Contains(nameLower, "card") || strings.Contains(nameLower, "payment")) &&
		(strings.Contains(nameLower, "last4") || strings.Contains(nameLower, "last_4") || strings.Contains(nameLower, "last-four")):
		return piiplaceholders.SyntheticCardLast4, true
	case strings.Contains(nameLower, "recipient") && !isIDField:
		return piiplaceholders.SyntheticRecipient, true
	case strings.Contains(nameLower, "address") && !isIDField:
		return piiplaceholders.SyntheticAddress, true
	}
	return "", false
}
