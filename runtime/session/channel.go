package session

import "strings"

// ChannelKeyMatches reports whether a job belongs to the given channel key.
func ChannelKeyMatches(jobChannelKey, contextChannelKey string) bool {
	return strings.TrimSpace(jobChannelKey) == strings.TrimSpace(contextChannelKey)
}
