package agentkit

import (
	"strconv"
	"strings"
)

func matchOptionLabels(text string, options []string) matchedOption {
	if len(options) == 0 {
		return matchedOption{Answered: true, text: text, selected: -1}
	}
	if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(options) {
		return matchedOption{Answered: true, text: options[n-1], selected: n - 1}
	}
	for i, opt := range options {
		if strings.EqualFold(text, strings.TrimSpace(opt)) {
			return matchedOption{Answered: true, text: opt, selected: i}
		}
	}
	return matchedOption{Answered: true, text: text, selected: -1}
}
