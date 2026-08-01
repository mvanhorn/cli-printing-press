package pipeline

// isAllowedDeadHelper reports whether name is a generated helper intentionally
// emitted into every printed CLI as an extension point, even when a particular
// generated tree does not call it yet. Keep this list narrow: entries here are
// excluded from checkDeadFunctions, scoreDeadCode, and findAllDeadFunctions.
func isAllowedDeadHelper(name string) bool {
	switch name {
	case "boundCtx": // used by hand-written novel commands; unused in endpoint-only CLIs
		return true
	case "declarePlatformAnalytics", // strict analytics declaration hook for hand-written novel commands
		"resolvePlatformWindow": // resolved-window hook for hand-written novel commands
		return true
	case "applyResponsePath",
		"cloneRawObject",
		"deleteRawPath",
		"emitMissingPaginationCursorWarning",
		"emitMissingPaginationSignalWarning",
		"emitPaginatedGetMaxPagesWarning",
		"emitTruncationWarning",
		"extractGraphQLConnection",
		"extractGraphQLObject",
		"formatCLIParamValue",
		"isJSONArray",
		"nextClientSidePaginationCursor",
		"nextFullPageOffsetCursor",
		"paginatedGet",
		"paginatedCollectionEnvelopeField",
		"paginationCursorToken",
		"replacePathParam",
		"responsePayloadParentAtPath",
		"writeNoop":
		return true
	default:
		return false
	}
}
