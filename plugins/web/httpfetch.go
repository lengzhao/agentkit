package web

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/lengzhao/agentkit/cap/web"
	"github.com/lengzhao/pluginkit"
)

const (
	defaultFetchTimeout  = 30 * time.Second
	defaultFetchMaxBytes = 1 << 20 // 1 MiB, same ceiling as tool/read-file
	defaultMaxRedirects  = 5
	defaultUserAgent     = "agentkit/1.0 (+https://github.com/lengzhao/agentkit)"
)

type FetchConfig struct {
	// TimeoutSeconds is per-request wall clock; defaults to 30.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// MaxBytes is body read limit before extraction; defaults to 1 MiB.
	MaxBytes int `json:"maxBytes,omitempty"`
	// MaxRedirects is redirect chain limit; defaults to 5.
	MaxRedirects int `json:"maxRedirects,omitempty"`
	// UserAgent overrides the outgoing User-Agent.
	UserAgent string `json:"userAgent,omitempty"`
	// AllowPrivateHosts allows loopback / private / link-local targets; off by default.
	AllowPrivateHosts bool `json:"allowPrivateHosts,omitempty"`
	// AllowHosts, when non-empty, only these hosts (and their subdomains) may be fetched.
	AllowHosts []string `json:"allowHosts,omitempty"`
	// DenyHosts are hosts to refuse, applied after AllowHosts.
	DenyHosts []string `json:"denyHosts,omitempty"`
}

type Fetcher struct {
	client       *http.Client
	maxBytes     int
	userAgent    string
	allowPrivate bool
	allowHosts   []string
	denyHosts    []string
}

func init() {
	pluginkit.Register("web/http-fetch", NewFetcher)
}

// NewFetcher registers web/http-fetch: Fetch a URL over HTTP(S) and return it as readable text.
//
// Best practices:
//   - Needs no credentials, so it is the one web provider that works in a keyless setup.
//   - Leave allowPrivateHosts off: it is what stops a fetched URL from reaching cloud metadata or internal admin endpoints.
//   - The private-address check runs at dial time, so it also covers redirects and DNS rebinding.
//   - Non-text responses are reported as a placeholder instead of being returned as bytes.
func NewFetcher(cfg FetchConfig) (web.Fetcher, error) {
	timeout := defaultFetchTimeout
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultFetchMaxBytes
	}
	maxRedirects := cfg.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = defaultMaxRedirects
	}
	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	f := &Fetcher{
		maxBytes:     maxBytes,
		userAgent:    userAgent,
		allowPrivate: cfg.AllowPrivateHosts,
		allowHosts:   normalizeHosts(cfg.AllowHosts),
		denyHosts:    normalizeHosts(cfg.DenyHosts),
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !cfg.AllowPrivateHosts {
		// Control runs after DNS resolution and once per connection attempt, so
		// it also covers redirects and rebinding tricks that a URL-level check
		// on the hostname would miss.
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("cannot parse dial address %q", address)
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("cannot parse resolved address %q", host)
			}
			if isPrivateIP(ip) {
				return fmt.Errorf("refusing to connect to non-public address %s (set allowPrivateHosts to override)", ip)
			}
			return nil
		}
	}

	f.client = &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return f.checkURL(req.URL)
		},
	}
	return f, nil
}

func (f *Fetcher) Fetch(ctx context.Context, req web.FetchRequest) (web.FetchResult, error) {
	target := strings.TrimSpace(req.URL)
	if target == "" {
		return web.FetchResult{}, fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return web.FetchResult{}, fmt.Errorf("invalid url %q: %w", target, err)
	}
	if err := f.checkURL(parsed); err != nil {
		return web.FetchResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return web.FetchResult{}, err
	}
	httpReq.Header.Set("User-Agent", f.userAgent)
	httpReq.Header.Set("Accept", "text/html,text/plain,application/json;q=0.9,*/*;q=0.5")

	resp, err := f.client.Do(httpReq)
	if err != nil {
		return web.FetchResult{}, fmt.Errorf("fetch %s: %w", parsed, err)
	}
	defer resp.Body.Close()

	maxBytes := f.maxBytes
	if req.MaxBytes > 0 && req.MaxBytes < maxBytes {
		maxBytes = req.MaxBytes
	}
	// Read one byte past the limit so truncation is detectable.
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return web.FetchResult{}, fmt.Errorf("read %s: %w", parsed, err)
	}
	truncated := len(body) > maxBytes
	if truncated {
		body = body[:maxBytes]
	}

	contentType := resp.Header.Get("Content-Type")
	result := web.FetchResult{
		URL:         resp.Request.URL.String(),
		Status:      resp.StatusCode,
		ContentType: contentType,
		Bytes:       len(body),
		Truncated:   truncated,
	}

	if !isTextual(contentType) {
		// Binary payloads are reported, not returned: handing a model a
		// megabyte of base64 helps nobody.
		result.Content = fmt.Sprintf("[non-text content: %s, %d bytes read]", contentType, len(body))
		return result, nil
	}

	text := string(body)
	if !req.Raw && isHTML(contentType) {
		result.Title = extractTitle(text)
		text = htmlToText(text)
	}
	result.Content = text
	return result, nil
}

// checkURL enforces the scheme and host rules. It runs on the requested URL and
// again on every redirect target.
func (f *Fetcher) checkURL(u *url.URL) error {
	switch u.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported url scheme %q (only http and https)", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("url %q has no host", u)
	}
	if len(f.allowHosts) > 0 && !matchHost(host, f.allowHosts) {
		return fmt.Errorf("host %q is not in allowHosts", host)
	}
	if matchHost(host, f.denyHosts) {
		return fmt.Errorf("host %q is in denyHosts", host)
	}
	// Literal non-public addresses are refused up front so the error names the
	// URL rather than surfacing as a dial failure.
	if !f.allowPrivate {
		if ip := net.ParseIP(host); ip != nil && isPrivateIP(ip) {
			return fmt.Errorf("refusing to fetch non-public address %s (set allowPrivateHosts to override)", ip)
		}
	}
	return nil
}

func normalizeHosts(hosts []string) []string {
	if len(hosts) == 0 {
		return nil
	}
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, ".")))
		if h != "" {
			out = append(out, h)
		}
	}
	return out
}

func matchHost(host string, list []string) bool {
	for _, entry := range list {
		if host == entry || strings.HasSuffix(host, "."+entry) {
			return true
		}
	}
	return false
}

// isPrivateIP reports whether ip is anything other than a routable public
// address — including the cloud metadata endpoints, which are link-local.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 100 && v4[1]&0xc0 == 64: // 100.64.0.0/10 CGNAT
			return true
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0: // 192.0.0.0/24
			return true
		case v4[0] == 198 && v4[1]&0xfe == 18: // 198.18.0.0/15 benchmarking
			return true
		case v4[0] >= 240: // 240.0.0.0/4 reserved + broadcast
			return true
		}
		return false
	}
	// fc00::/7 unique-local; net.IP.IsPrivate covers fc00::/7 already on new
	// Go versions, this stays as an explicit belt for older byte layouts.
	if len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc {
		return true
	}
	return false
}

func isTextual(contentType string) bool {
	mime := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mime == "" {
		// Servers that omit Content-Type are common enough; treat as text and
		// let the model judge the body.
		return true
	}
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	switch mime {
	case "application/json", "application/xml", "application/xhtml+xml",
		"application/javascript", "application/x-ndjson", "application/yaml",
		"application/rss+xml", "application/atom+xml":
		return true
	}
	return strings.HasSuffix(mime, "+json") || strings.HasSuffix(mime, "+xml")
}

func isHTML(contentType string) bool {
	mime := strings.ToLower(contentType)
	return strings.Contains(mime, "text/html") || strings.Contains(mime, "xhtml")
}
