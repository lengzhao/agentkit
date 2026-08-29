package chatapi

import (
	"net/http"
	"net/url"
	"strings"
)

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = strings.TrimSpace(strings.Split(proto, ",")[0])
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

// apiBaseFromRequest returns the API prefix clients should call, e.g.
// https://api.example.com/v1. Config publicBaseURL wins when set.
func (p *Platform) apiBaseFromRequest(r *http.Request) string {
	if base := strings.TrimRight(strings.TrimSpace(p.publicBaseURL), "/"); base != "" {
		return base
	}
	if r == nil {
		return ""
	}
	origin := requestOrigin(r)
	if origin == "" {
		return ""
	}
	return origin + strings.TrimRight(p.path, "/")
}

func (p *Platform) fileDownloadPath(fileID string) string {
	return strings.TrimRight(p.path, "/") + "/files/" + fileID
}

func (p *Platform) fileDownloadURL(apiBase, channel, fileID string) string {
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if apiBase == "" {
		return ""
	}
	u := apiBase + "/files/" + fileID
	q := url.Values{}
	if channel != "" {
		q.Set("channel", channel)
	}
	if qs := q.Encode(); qs != "" {
		u += "?" + qs
	}
	return u
}

func (p *Platform) fileLinkFields(apiBase, channel, fileID string) map[string]string {
	out := map[string]string{"path": p.fileDownloadPath(fileID)}
	if u := p.fileDownloadURL(apiBase, channel, fileID); u != "" {
		out["url"] = u
	}
	return out
}

func mergeFilePayload(base map[string]any, links map[string]string) map[string]any {
	for k, v := range links {
		if v != "" {
			base[k] = v
		}
	}
	return base
}
