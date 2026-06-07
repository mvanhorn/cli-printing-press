package pipeline

func isAllowedDeadHelper(name string) bool {
	switch name {
	case "boundCtx":
		return true
	default:
		return false
	}
}
