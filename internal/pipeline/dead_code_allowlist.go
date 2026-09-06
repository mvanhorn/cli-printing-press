package pipeline

// isAllowedDeadHelper reports whether name is a generated helper intentionally
// emitted into every printed CLI as an extension point, even when a particular
// generated tree does not call it yet. Keep this list narrow: entries here are
// excluded from checkDeadFunctions, scoreDeadCode, and findAllDeadFunctions.
func isAllowedDeadHelper(name string) bool {
	switch name {
	case "boundCtx", // used by hand-written novel commands; unused in endpoint-only CLIs
		"writeHarnessRefusal": // structured side-effect refusal hook for hand-written novel commands
		return true
	case "redirectLeavesOrigin": // shared CheckRedirect origin gate; unused when no site strips or re-stamps
		return true
	case "declarePlatformAnalytics", // strict analytics declaration hook for hand-written novel commands
		"resolvePlatformWindow": // resolved-window hook for hand-written novel commands
		return true
	case "applyResponsePath",
		"cloneRawObject",
		"cursorFromNextURL",
		"cursorTokenFromMaybeURL",
		"deleteRawPath",
		"emitMissingPaginationCursorWarning",
		"emitMissingPaginationSignalWarning",
		"emitPaginatedGetMaxPagesWarning",
		"emitPaginatedGetRepeatedPageWarning",
		"emitTruncationWarning",
		"extractGraphQLConnection",
		"extractGraphQLObject",
		"formatCLIParamValue",
		"isFollowableNextURL",
		"isJSONArray",
		"nextClientSidePaginationCursor",
		"nextCursorFromLinks",
		"nextCursorFromTopLevelURL",
		"nextFullPageOffsetCursor",
		"paginatedGet",
		"paginatedCollectionEnvelopeField",
		"paginatedItemsEqual",
		"paginationCursorToken",
		"paginationLinkURL",
		"pathParamSegmentValue",
		"replaceDependentPathParam",
		"replacePathParam",
		"replaceURLIDPathParam",
		"reportedCollectionTotal",
		"reportedCollectionTotalAtDepth",
		"resolvePaginatedNextCursor",
		"resourceURLIDPathParam",
		"responsePayloadParentAtPath",
		"urlIDFieldName",
		"writeNoop":
		return true
	default:
		return false
	}
}
