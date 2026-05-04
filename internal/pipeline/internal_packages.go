package pipeline

import "regexp"

// reservedInternalPackages names the internal packages the generator emits
// unconditionally. Files under any of these are not agent-authored, so dogfood
// checks that look for hand-rolled API behavior must skip them.
var reservedInternalPackages = map[string]bool{
	"client":   true,
	"store":    true,
	"cliutil":  true,
	"cache":    true,
	"config":   true,
	"mcp":      true,
	"types":    true,
	"share":    true,
	"deliver":  true,
	"profile":  true,
	"feedback": true,
	"graphql":  true,
}

const (
	rawOutboundHTTPCallPattern = `\bhttp\.(?:Get|Post|NewRequest(?:WithContext)?|Do)\s*\(|` +
		`\b\w+\.HTTPClient\.Do\s*\(|` +
		`\b\w+\.HTTP\.Do\s*\(`
	generatedClientReceiverCallPattern = `\bc\.(?:Do|Get|Post)\s*\(`
)

// rawOutboundHTTPCallRe matches outbound HTTP request shapes that bypass the
// generated client. Files with these calls need local limiter and typed 429
// handling when they live in hand-written sibling internal packages.
var rawOutboundHTTPCallRe = regexp.MustCompile(rawOutboundHTTPCallPattern)

var generatedClientParamRe = regexp.MustCompile(`\bc\s+\*client\.Client\b`)
var generatedClientReceiverCallRe = regexp.MustCompile(generatedClientReceiverCallPattern)

// outboundHTTPCallRe matches every outbound HTTP request shape that appears in
// generated and agent-authored Go code. Centralized so reimplementation_check
// (per-command) and source_client_check (per-sibling-package) cannot diverge.
var outboundHTTPCallRe = regexp.MustCompile(rawOutboundHTTPCallPattern + `|` + generatedClientReceiverCallPattern)
