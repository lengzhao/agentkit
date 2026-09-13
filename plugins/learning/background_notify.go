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

func isStagedCaptureNotice(msg string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(msg)), "staged for approval")
}

func partitionCaptureNotices(notices []string) (committed, staged []string) {
	for _, n := range notices {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if isStagedCaptureNotice(n) {
			staged = append(staged, n)
		} else {
			committed = append(committed, n)
		}
	}
	return committed, staged
}

// FormatBackgroundReviewNotification builds a short chat line after review writes.
func FormatBackgroundReviewNotification(mode string, notices []string) string {
	mode = NormalizeMemoryNotifications(mode)
	if mode == "off" {
		return ""
	}
	committed, staged := partitionCaptureNotices(notices)
	if len(committed) > 0 {
		if mode == "verbose" {
			preview := strings.TrimSpace(committed[0])
			if len(preview) > 120 {
				preview = preview[:120] + "…"
			}
			if strings.Contains(strings.ToLower(preview), "skill") {
				return "💾 " + preview
			}
			return "💾 Memory ➕ " + preview
		}
		for _, n := range committed {
			if strings.Contains(strings.ToLower(n), "skill") {
				return "💾 Skill updated"
			}
		}
		return "💾 Memory updated"
	}
	if len(staged) == 0 {
		return ""
	}
	if mode == "verbose" {
		preview := strings.TrimSpace(staged[0])
		if len(preview) > 100 {
			preview = preview[:100] + "…"
		}
		return "💾 Memory pending approval: " + preview
	}
	return "💾 Memory pending approval"
}
