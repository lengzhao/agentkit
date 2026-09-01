package agentkit

import "context"

const (
	// KeyIsAdmin marks whether the current user is configured as an admin.
	KeyIsAdmin contextKey = "agentkit.is_admin"
)

// IsAdmin reports whether ctx carries admin privileges for the current user.
func IsAdmin(ctx context.Context) bool {
	v, _ := ctx.Value(KeyIsAdmin).(bool)
	return v
}
