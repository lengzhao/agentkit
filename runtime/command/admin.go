package command

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// IsAdmin reports whether ctx carries admin privileges for the current user.
func IsAdmin(ctx context.Context) bool {
	v, _ := ctx.Value(agentkit.KeyIsAdmin).(bool)
	return v
}
