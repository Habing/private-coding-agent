package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// HTTPFetchConfig controls server-side outbound HTTP (Slice 25a).
type HTTPFetchConfig struct {
	Enabled         bool
	AllowHosts      []string
	TimeoutSec      int
	MaxBodyBytes    int64
	BlockPrivateIPs bool
}

// HTTPFetch is the server-side http.fetch tool with a runtime-updatable host allowlist.
type HTTPFetch struct {
	mu              sync.RWMutex
	allow           []string
	timeout         time.Duration
	maxBody         int64
	blockPrivateIPs bool
	client          *http.Client
}

// NewHTTPFetch registers http.fetch when cfg.Enabled. Returns nil if disabled.
func NewHTTPFetch(cfg HTTPFetchConfig) *HTTPFetch {
	if !cfg.Enabled {
		return nil
	}
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = 512 * 1024
	}
	return &HTTPFetch{
		allow:           append([]string(nil), cfg.AllowHosts...),
		timeout:         timeout,
		maxBody:         maxBody,
		blockPrivateIPs: cfg.BlockPrivateIPs,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

// AllowHosts returns a copy of the current host allowlist.
func (t *HTTPFetch) AllowHosts() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return append([]string(nil), t.allow...)
}

// SetAllowHosts replaces the runtime host allowlist.
func (t *HTTPFetch) SetAllowHosts(hosts []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.allow = append([]string(nil), hosts...)
}

// BlockPrivateIPs reports whether SSRF private-IP blocking is enabled.
func (t *HTTPFetch) BlockPrivateIPs() bool {
	return t.blockPrivateIPs
}

func (t *HTTPFetch) Name() string { return "http.fetch" }
func (t *HTTPFetch) Description() string {
	return "从 server 侧发起 HTTP 请求并返回响应（沙箱无需出网）。仅允许配置中的 host。"
}
func (t *HTTPFetch) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "url":{"type":"string","description":"https:// 或 http:// 完整 URL"},
            "method":{"type":"string","enum":["GET","POST","HEAD"],"default":"GET"},
            "headers":{"type":"object","additionalProperties":{"type":"string"}},
            "body":{"type":"string","description":"可选请求体（POST）"}
        },
        "required":["url"],
        "additionalProperties":false
    }`)
}

type httpFetchIn struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type httpFetchOut struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	Truncated  bool              `json:"truncated"`
	URL        string            `json:"url"`
}

func (t *HTTPFetch) Invoke(ctx context.Context, _, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in httpFetchIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if strings.TrimSpace(in.URL) == "" {
		return nil, fmt.Errorf("%w: url required", toolbus.ErrInvalidArguments)
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodHead:
	default:
		return nil, fmt.Errorf("%w: unsupported method %q", toolbus.ErrInvalidArguments, in.Method)
	}

	parsed, err := url.Parse(in.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid url: %v", toolbus.ErrInvalidArguments, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: only http/https allowed", toolbus.ErrInvalidArguments)
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("%w: missing host", toolbus.ErrInvalidArguments)
	}
	t.mu.RLock()
	allow := append([]string(nil), t.allow...)
	t.mu.RUnlock()
	if !hostAllowed(host, allow) {
		return nil, fmt.Errorf("%w: host %q not in allowlist", toolbus.ErrInvalidArguments, host)
	}
	if t.blockPrivateIPs {
		if err := assertPublicHost(ctx, host); err != nil {
			return nil, err
		}
	}

	var bodyReader io.Reader
	if in.Body != "" && method == http.MethodPost {
		bodyReader = strings.NewReader(in.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}
	if method == http.MethodPost && in.Body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, t.maxBody+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	truncated := int64(len(raw)) > t.maxBody
	if truncated {
		raw = raw[:t.maxBody]
	}

	hdr := map[string]string{}
	for k, vals := range resp.Header {
		if len(vals) > 0 {
			hdr[k] = vals[0]
		}
	}
	out := httpFetchOut{
		StatusCode: resp.StatusCode,
		Headers:    hdr,
		Body:       string(raw),
		Truncated:  truncated,
		URL:        parsed.String(),
	}
	return json.Marshal(out)
}

func hostAllowed(host string, allow []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, pattern := range allow {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if pattern == "*" {
			return true
		}
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			if host == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(host, suffix) {
				return true
			}
			continue
		}
		if host == pattern {
			return true
		}
	}
	return false
}

func assertPublicHost(ctx context.Context, host string) error {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: dns lookup failed: %v", toolbus.ErrInvalidArguments, err)
	}
	for _, ip := range ips {
		if ip.IP.IsLoopback() || ip.IP.IsPrivate() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsLinkLocalMulticast() {
			return fmt.Errorf("%w: host resolves to non-public IP", toolbus.ErrInvalidArguments)
		}
	}
	return nil
}
