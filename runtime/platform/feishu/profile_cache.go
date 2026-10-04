package feishu

// ClearUserProfileCache removes all cached Contact API profile entries for this leaf.
// Call after fixing app scopes or when stale empty profiles were cached.
func (p *Platform) ClearUserProfileCache() int {
	if p == nil {
		return 0
	}
	n := 0
	p.userProfileCache.Range(func(key, _ any) bool {
		p.userProfileCache.Delete(key)
		n++
		return true
	})
	return n
}
