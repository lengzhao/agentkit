package chatapi

import (
	"net/http"
	"strings"
)

const defaultMetadataHeaderTaskID = "x-task-id"

func resolveMetadataHeaders(configured []string) []string {
	headers := append([]string{defaultMetadataHeaderTaskID}, configured...)
	return normalizeMetadataHeaders(headers)
}

func normalizeMetadataHeaders(headers []string) []string {
	if len(headers) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(headers))
	out := make([]string, 0, len(headers))
	for _, h := range headers {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		key := strings.ToLower(h)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (p *Platform) metadataFromRequest(r *http.Request) map[string]any {
	if len(p.metadataHeaders) == 0 || r == nil {
		return nil
	}
	md := make(map[string]any)
	for _, h := range p.metadataHeaders {
		v := strings.TrimSpace(r.Header.Get(h))
		if v == "" {
			continue
		}
		md[h] = v
	}
	if len(md) == 0 {
		return nil
	}
	return md
}

func (p *Platform) corsAllowedHeaders() []string {
	headers := []string{
		"Authorization", "Content-Type", "Accept",
		p.userHeader, p.userNameHeader, p.channelHeader,
	}
	headers = append(headers, p.metadataHeaders...)
	seen := make(map[string]struct{}, len(headers))
	out := make([]string, 0, len(headers))
	for _, h := range headers {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}
