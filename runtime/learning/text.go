package learning

// TruncateEllipsis shortens s to at most max bytes and appends an ellipsis rune.
func TruncateEllipsis(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
