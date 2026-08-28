package common

import (
	"fmt"
	"net/url"
	"strings"
)

// ResolveDomain returns trimmed raw when set, otherwise defaultDomain.
// Empty raw after trim uses defaultDomain without validation.
func ResolveDomain(raw, defaultDomain string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultDomain, nil
	}
	if _, err := url.ParseRequestURI(raw); err != nil {
		return "", fmt.Errorf("invalid domain %q: %w", raw, err)
	}
	return strings.TrimRight(raw, "/"), nil
}

// NormalizeSlackAPIURL returns a slack-go compatible Web API base URL (trailing slash).
// Bare host URLs are normalized to end with /api/ like cc-connect.
func NormalizeSlackAPIURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", fmt.Errorf("invalid domain %q: must be a valid http(s) URL", raw)
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	if path == "" || !strings.HasSuffix(path, "/api") {
		if path == "" {
			path = "/api"
		} else {
			path = path + "/api"
		}
	}
	parsed.Path = path + "/"
	return parsed.String(), nil
}
