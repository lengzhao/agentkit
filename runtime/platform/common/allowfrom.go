package common

import (
	"log/slog"
	"strings"
)

const UnauthorizedMessage = "角色未授权，请联系管理员添加权限。"

// WarnAllowFromEmpty logs when allow_from is unset (permit-all).
func WarnAllowFromEmpty(platform, allowFrom string) {
	if strings.TrimSpace(allowFrom) == "" {
		slog.Warn("allow_from is not set — all users are permitted. Set allow_from in config to restrict access.",
			"platform", platform)
	}
}

// AllowList returns true when userID is permitted by comma-separated allow_from.
// Empty or "*" means allow all.
func AllowList(allowFrom, userID string) bool {
	allowFrom = strings.TrimSpace(allowFrom)
	if allowFrom == "" || allowFrom == "*" {
		return true
	}
	for _, id := range strings.Split(allowFrom, ",") {
		if strings.EqualFold(strings.TrimSpace(id), userID) {
			return true
		}
	}
	return false
}
