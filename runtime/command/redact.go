package command

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

func sanitizeArgsForLog(cmd agentkit.Command, rawArgs string) string {
	args := strings.TrimSpace(rawArgs)
	if cmd != nil {
		if s, ok := cmd.(agentkit.CommandLogSanitizer); ok {
			args = s.SanitizeArgsForLog(args)
		}
	}
	const maxLen = 200
	if len(args) <= maxLen {
		return args
	}
	return args[:maxLen] + "…"
}
