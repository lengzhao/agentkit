package interaction

import (
	"strconv"
	"strings"
)

type matchedLabel struct {
	answered bool
	text     string
	selected int
}

func matchLabels(text string, options []string) matchedLabel {
	if len(options) == 0 {
		return matchedLabel{answered: true, text: text, selected: -1}
	}
	if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(options) {
		return matchedLabel{answered: true, text: options[n-1], selected: n - 1}
	}
	for i, opt := range options {
		if strings.EqualFold(text, strings.TrimSpace(opt)) {
			return matchedLabel{answered: true, text: opt, selected: i}
		}
	}
	return matchedLabel{answered: true, text: text, selected: -1}
}
