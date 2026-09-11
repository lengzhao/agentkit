package agentkit

import "strings"

// SlashLogRedacted is the placeholder for sanitized slash command args in dispatch logs.
const SlashLogRedacted = "<redacted>"

// RedactSlashArgsForLog replaces non-empty args with SlashLogRedacted.
func RedactSlashArgsForLog(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return args
	}
	return SlashLogRedacted
}

// RedactSlashAddNamePayload redacts JSON (or other tail) after add [-g] <name> for /mcp and /openapi style commands.
func RedactSlashAddNamePayload(args string) string {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 || fields[0] != "add" {
		return args
	}
	i := 1
	if i < len(fields) && fields[i] == "-g" {
		i++
	}
	if i >= len(fields) {
		return "add " + SlashLogRedacted
	}
	i++
	if i >= len(fields) {
		return strings.Join(fields[:i], " ")
	}
	return strings.Join(fields[:i], " ") + " " + SlashLogRedacted
}
