package spec

import "strings"

// nonCreateActionTokens are RPC verbs. Matching is exact, so a plural
// such as "uploads" is not the "upload" action.
var nonCreateActionTokens = map[string]struct{}{
	"approve":   {},
	"archive":   {},
	"batch":     {},
	"block":     {},
	"cancel":    {},
	"close":     {},
	"count":     {},
	"delete":    {},
	"describe":  {},
	"disable":   {},
	"download":  {},
	"edit":      {},
	"enable":    {},
	"execute":   {},
	"fetch":     {},
	"find":      {},
	"freeze":    {},
	"generate":  {},
	"get":       {},
	"list":      {},
	"process":   {},
	"publish":   {},
	"purge":     {},
	"query":     {},
	"refund":    {},
	"reject":    {},
	"remove":    {},
	"restore":   {},
	"revoke":    {},
	"run":       {},
	"search":    {},
	"send":      {},
	"submit":    {},
	"trigger":   {},
	"unarchive": {},
	"unfreeze":  {},
	"update":    {},
	"upload":    {},
	"verify":    {},
}

var createLikeTokens = map[string]struct{}{
	"add":    {},
	"create": {},
	"insert": {},
}

// CreateShapedPOST reports whether a POST should take the create alias.
// A missing alias is safe. An action that answers to create is not.
// A path parameter on an earlier segment does not block a nested
// collection whose name is create-like; a final {param} segment does.
func CreateShapedPOST(path, endpointName string) bool {
	if pathSignalsAction(path) {
		return false
	}
	createLike := false
	for _, token := range endpointNameTokens(endpointName) {
		if _, action := nonCreateActionTokens[token]; action {
			return false
		}
		if _, ok := createLikeTokens[token]; ok {
			createLike = true
		}
	}
	if createLike {
		return true
	}
	return !strings.Contains(path, "{")
}

func pathSignalsAction(path string) bool {
	if strings.Contains(path, ":") {
		return true
	}
	last, ok := lastPathSegment(path)
	if !ok {
		return false
	}
	if strings.HasPrefix(last, "{") && strings.HasSuffix(last, "}") {
		return true
	}
	_, action := nonCreateActionTokens[last]
	return action
}

func lastPathSegment(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	path = strings.Trim(path, "/")
	if path == "" {
		return "", false
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		path = path[i+1:]
	}
	return strings.ToLower(path), true
}

func endpointNameTokens(name string) []string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil
	}
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
