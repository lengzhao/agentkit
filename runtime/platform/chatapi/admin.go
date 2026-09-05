package chatapi

import "strings"

func (p *Platform) isAdminUser(userID string) bool {
	if len(p.admins) == 0 {
		return false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	for _, id := range p.admins {
		if strings.EqualFold(strings.TrimSpace(id), userID) {
			return true
		}
	}
	return false
}
