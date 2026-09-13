package learning

import "strings"

// NormalizeMemoryNotifications maps hook config to off | on | verbose.
func NormalizeMemoryNotifications(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "off", "on", "verbose":
		return mode
	default:
		return "off"
	}
}

// FormatBackgroundReviewNotification builds a short chat line after review writes.
func FormatBackgroundReviewNotification(mode string, notices []string) string {
	mode = NormalizeMemoryNotifications(mode)
	if mode == "off" || len(notices) == 0 {
		return ""
	}
	if mode == "verbose" {
		preview := strings.TrimSpace(notices[0])
		if len(preview) > 120 {
			preview = preview[:120] + "…"
		}
		if strings.Contains(strings.ToLower(preview), "skill") {
			return "💾 " + preview
		}
		return "💾 Memory ➕ " + preview
	}
	for _, n := range notices {
		if strings.Contains(strings.ToLower(n), "skill") {
			return "💾 Skill updated"
		}
	}
	return "💾 Memory updated"
}
