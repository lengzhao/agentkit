package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// prepareEndpoint rewrites a mcp.json url into the endpoint the MCP HTTP
// client dials. http and https URLs are returned unchanged with a nil client.
// unix:///<socket>[:/<http-path>] dials the socket and speaks HTTP/1.1 to
// http://localhost/<http-path>. The HTTP path defaults to / when omitted.
// Both SSE and streamable HTTP use this client.
func prepareEndpoint(raw string) (string, *http.Client, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), "unix:") {
		return raw, nil, nil
	}
	socketPath, endpoint, err := parseUnixURL(raw)
	if err != nil {
		return "", nil, err
	}
	return endpoint, unixHTTPClient(socketPath), nil
}

func parseUnixURL(raw string) (socketPath, endpoint string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse unix url: %w", err)
	}
	if u.Host != "" {
		return "", "", fmt.Errorf("unix url %q must be unix:///<socket>[:/<http-path>]", raw)
	}
	socketPath, httpPath := splitUnixPath(u.Path)
	if socketPath == "" || !strings.HasPrefix(socketPath, "/") {
		return "", "", fmt.Errorf("unix url %q needs an absolute socket path", raw)
	}
	if httpPath == "" {
		httpPath = "/"
	}
	endpointURL := url.URL{
		Scheme:   "http",
		Host:     "localhost",
		Path:     httpPath,
		RawQuery: u.RawQuery,
		Fragment: u.Fragment,
	}
	return socketPath, endpointURL.String(), nil
}

// splitUnixPath separates an absolute socket path from the HTTP path.
// The HTTP path is the suffix after the last ":" when that suffix is empty
// or starts with "/": /tmp/mcp.sock:/mcp → /tmp/mcp.sock, /mcp.
// A colon that is part of the socket name (…/foo:bar.sock) stays in the socket path.
func splitUnixPath(path string) (socketPath, httpPath string) {
	if i := strings.LastIndex(path, ":"); i >= 0 {
		rest := path[i+1:]
		if rest == "" || strings.HasPrefix(rest, "/") {
			if rest == "" {
				rest = "/"
			}
			return path[:i], rest
		}
	}
	return path, "/"
}

func unixHTTPClient(socketPath string) *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	return &http.Client{
		Transport: &http.Transport{
			// Proxy would dial the proxy instead of the socket.
			Proxy: nil,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, "unix", socketPath)
			},
			IdleConnTimeout: 90 * time.Second,
		},
	}
}
