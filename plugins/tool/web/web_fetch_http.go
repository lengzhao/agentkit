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

	"github.com/lengzhao/agentkit"
)

const (
	defaultFetchTimeout  = 30 * time.Second
	defaultFetchMaxBytes = 1 << 20
	defaultMaxRedirects  = 5
	defaultUserAgent     = "agentkit/1.0 (+https://github.com/lengzhao/agentkit)"
)

type WebFetchHTTPConfig struct {
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

type httpFetcher struct {
	client       *http.Client
	maxBytes     int
	userAgent    string
	allowPrivate bool
	allowHosts   []string
	denyHosts    []string
}

type WebFetchInput struct {
	URL string `json:"url" jsonschema:"Absolute http or https URL to fetch"`
	Raw bool   `json:"raw,omitempty" jsonschema:"Return the body as served instead of extracting readable text from HTML"`
}

type WebFetchOutput struct {
	URL         string `json:"url"`
	Status      int    `json:"status"`
	ContentType string `json:"contentType,omitempty"`
	Title       string `json:"title,omitempty"`
	Content     string `json:"content"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// NewWebFetchHTTP registers tool/web-fetch-http: Fetch a URL over HTTP(S) and return readable text (tool name: web_fetch).
//
// Best practices:
//   - Needs no credentials, so it works without any API key.
//   - Leave allowPrivateHosts off to block cloud metadata and internal admin endpoints.
func NewWebFetchHTTP(cfg WebFetchHTTPConfig) (agentkit.ToolPack, error) {
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

	f := &httpFetcher{
		maxBytes:     maxBytes,
		userAgent:    userAgent,
		allowPrivate: cfg.AllowPrivateHosts,
		allowHosts:   normalizeHosts(cfg.AllowHosts),
		denyHosts:    normalizeHosts(cfg.DenyHosts),
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !cfg.AllowPrivateHosts {
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

	tool, err := agentkit.NewTool[WebFetchInput, WebFetchOutput]("web_fetch", func(ctx context.Context, input WebFetchInput) (WebFetchOutput, error) {
		url := strings.TrimSpace(input.URL)
		if url == "" {
			return WebFetchOutput{}, fmt.Errorf("url is required")
		}
		result, err := f.fetch(ctx, url, input.Raw)
		if err != nil {
			return WebFetchOutput{}, err
		}
		return result, nil
	}).Description("Fetch a URL and return its readable text. Use it to read a page you already have the address for; cite the returned url when you use the content.").Build()
	if err != nil {
		return nil, err
	}
	return agentkit.Pack(tool), nil
}

func (f *httpFetcher) fetch(ctx context.Context, target string, raw bool) (WebFetchOutput, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return WebFetchOutput{}, fmt.Errorf("invalid url %q: %w", target, err)
	}
	if err := f.checkURL(parsed); err != nil {
		return WebFetchOutput{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return WebFetchOutput{}, err
	}
	httpReq.Header.Set("User-Agent", f.userAgent)
	httpReq.Header.Set("Accept", "text/html,text/plain,application/json;q=0.9,*/*;q=0.5")

	resp, err := f.client.Do(httpReq)
	if err != nil {
		return WebFetchOutput{}, fmt.Errorf("fetch %s: %w", parsed, err)
	}
	defer resp.Body.Close()

	maxBytes := f.maxBytes
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return WebFetchOutput{}, fmt.Errorf("read %s: %w", parsed, err)
	}
	truncated := len(body) > maxBytes
	if truncated {
		body = body[:maxBytes]
	}

	contentType := resp.Header.Get("Content-Type")
	result := WebFetchOutput{
		URL:         resp.Request.URL.String(),
		Status:      resp.StatusCode,
		ContentType: contentType,
		Truncated:   truncated,
	}

	if !isTextual(contentType) {
		result.Content = fmt.Sprintf("[non-text content: %s, %d bytes read]", contentType, len(body))
		return result, nil
	}

	text := string(body)
	if !raw && isHTML(contentType) {
		result.Title = extractTitle(text)
		text = htmlToText(text)
	}
	result.Content = text
	return result, nil
}

func (f *httpFetcher) checkURL(u *url.URL) error {
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

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 100 && v4[1]&0xc0 == 64:
			return true
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0:
			return true
		case v4[0] == 198 && v4[1]&0xfe == 18:
			return true
		case v4[0] >= 240:
			return true
		}
		return false
	}
	if len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc {
		return true
	}
	return false
}

func isTextual(contentType string) bool {
	mime := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mime == "" {
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
