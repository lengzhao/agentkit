package common

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// MentionProfile is a resolved @mention participant for inbound metadata inject.
type MentionProfile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UserProfileMetadata builds inbound sender metadata for runner inject.
// Leaf platforms query user APIs and set displayName/email; chat-api maps
// caller headers into Metadata instead.
func UserProfileMetadata(name, email string) map[string]any {
	md := make(map[string]any, 2)
	if name = strings.TrimSpace(name); name != "" {
		md["displayName"] = name
	}
	if email = strings.TrimSpace(email); email != "" {
		md["email"] = email
	}
	if len(md) == 0 {
		return nil
	}
	return md
}

// MentionProfilesMetadata builds inbound mention metadata for runner inject.
func MentionProfilesMetadata(profiles []MentionProfile) map[string]any {
	if len(profiles) == 0 {
		return nil
	}
	items := make([]map[string]string, 0, len(profiles))
	for _, profile := range profiles {
		item := map[string]string{}
		if id := strings.TrimSpace(profile.ID); id != "" {
			item["id"] = id
		}
		if name := strings.TrimSpace(profile.Name); name != "" {
			item["name"] = name
		}
		if email := strings.TrimSpace(profile.Email); email != "" {
			item["email"] = email
		}
		if len(item) == 0 {
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil
	}
	return map[string]any{"mentions": items}
}

// MergeMetadata merges extra metadata into base without overwriting existing keys.
func MergeMetadata(base, extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return base
	}
	if len(base) == 0 {
		return extra
	}
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}

// WithMetadata merges metadata onto an inbound event without overwriting existing keys.
func WithMetadata(event agentkit.MessageEvent, metadata map[string]any) agentkit.MessageEvent {
	if len(metadata) == 0 {
		return event
	}
	if event.Metadata == nil {
		event.Metadata = metadata
		return event
	}
	for k, v := range metadata {
		if _, ok := event.Metadata[k]; !ok {
			event.Metadata[k] = v
		}
	}
	return event
}
