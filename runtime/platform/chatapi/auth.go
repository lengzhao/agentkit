package chatapi

import (
	"net/http"
	"strings"
)

const (
	defaultUserHeader     = "X-Chat-API-User"
	defaultUserNameHeader = "X-Chat-API-User-Name"
	defaultChannelHeader  = "X-Chat-API-Channel"
	maxUserLen            = 128
	maxChannelLen         = 256
)

func (p *Platform) authHTTP(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p.apiToken != "" && !p.isAnonymousFileDownload(r) && !p.authenticated(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func (p *Platform) isAnonymousFileDownload(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	prefix := strings.TrimRight(p.path, "/") + "/files/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if id == "" || strings.Contains(id, "/") {
		return false
	}
	return fileIDPattern.MatchString(id)
}

func (p *Platform) authenticated(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	token, ok := strings.CutPrefix(auth, "Bearer ")
	return ok && strings.TrimSpace(token) == p.apiToken
}

func (p *Platform) corsHTTP(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(p.corsOrigins) > 0 {
			origin := r.Header.Get("Origin")
			for _, allowed := range p.corsOrigins {
				if allowed == "*" || origin == allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", strings.Join(p.corsAllowedHeaders(), ", "))
					break
				}
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (p *Platform) resolveUser(w http.ResponseWriter, r *http.Request, writeOnly bool) (string, bool) {
	if writeOnly {
		user := strings.TrimSpace(r.Header.Get(p.userHeader))
		if user == "" {
			writeErr(w, http.StatusBadRequest, "user required")
			return "", false
		}
		if !validUser(user) {
			writeErr(w, http.StatusBadRequest, "invalid request")
			return "", false
		}
		return user, true
	}
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	if user == "" {
		user = strings.TrimSpace(r.Header.Get(p.userHeader))
	}
	if user == "" {
		writeErr(w, http.StatusBadRequest, "user required")
		return "", false
	}
	if !validUser(user) {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return "", false
	}
	return user, true
}

func optionalUser(r *http.Request, userHeader string) string {
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	if user == "" {
		user = strings.TrimSpace(r.Header.Get(userHeader))
	}
	if !validUser(user) {
		return ""
	}
	return user
}

func (p *Platform) resolveChannel(w http.ResponseWriter, r *http.Request) (string, bool) {
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	if channel == "" {
		channel = strings.TrimSpace(r.Header.Get(p.channelHeader))
	}
	if channel == "" {
		writeErr(w, http.StatusBadRequest, "channel required")
		return "", false
	}
	if !validChannel(channel) {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return "", false
	}
	return channel, true
}

func validUser(user string) bool {
	if user == "" || len(user) > maxUserLen {
		return false
	}
	for _, r := range user {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_', r == '-', r == ':', r == '.':
		default:
			return false
		}
	}
	return true
}

func validChannel(channel string) bool {
	if channel == "" || len(channel) > maxChannelLen {
		return false
	}
	if strings.Contains(channel, "..") {
		return false
	}
	for _, r := range channel {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.', r == '/':
		default:
			return false
		}
	}
	return true
}
