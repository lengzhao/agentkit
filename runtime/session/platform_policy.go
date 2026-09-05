package session

import "strings"

// PlatformSessionPolicy holds per-platform routing overrides applied on top of
// runner defaults.
type PlatformSessionPolicy struct {
	ActiveEntryMode ActiveEntryMode
}

var platformSessionPolicies = map[string]PlatformSessionPolicy{
	"chat-api": {ActiveEntryMode: ActiveEntryDelivery},
}

// RegisterPlatformPolicy registers or overrides routing policy for a platform id.
func RegisterPlatformPolicy(platform string, policy PlatformSessionPolicy) {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return
	}
	platformSessionPolicies[platform] = policy
}

// PlatformSessionPolicyFor returns overrides for a platform, or zero when unset.
func PlatformSessionPolicyFor(platform string) PlatformSessionPolicy {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return PlatformSessionPolicy{}
	}
	return platformSessionPolicies[platform]
}

// RoutePolicyForPlatform merges runner defaults with platform-specific overrides.
func RoutePolicyForPlatform(platform string, base RoutePolicy) RoutePolicy {
	if p := PlatformSessionPolicyFor(platform); p.ActiveEntryMode != "" {
		base.ActiveEntryMode = p.ActiveEntryMode
	}
	return base
}
