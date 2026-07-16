// Package outbound builds HTTP clients for all egress to xAI (OAuth + API).
//
// Proxy policy (highest wins):
//  1. Explicit Options.ProxyURL / CLI --proxy / XAI_PROXY_OUTBOUND
//  2. Environment: ALL_PROXY, HTTPS_PROXY, HTTP_PROXY (and lowercase)
//  3. Direct connection
//
// Supported schemes: http, https, socks5, socks5h (socks:// treated as socks5).
// NO_PROXY / no_proxy is honored for both HTTP and SOCKS modes.
package outbound

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/proxy"
)

// Options configures outbound client construction.
type Options struct {
	// ProxyURL is an explicit proxy (http://, https://, socks5://, socks5h://).
	// Empty means fall back to environment variables.
	ProxyURL string
	// Timeout is the client-level timeout (0 = no total timeout; use for streaming).
	Timeout time.Duration
}

// defaultExplicit is set by CLI once for process-wide default when packages
// construct clients without Options (auth refresh paths).
var defaultExplicit string

// SetDefaultProxyURL sets the process-wide explicit outbound proxy (CLI --proxy).
func SetDefaultProxyURL(u string) {
	defaultExplicit = strings.TrimSpace(u)
}

// DefaultProxyURL returns the process-wide explicit proxy, if any.
func DefaultProxyURL() string {
	return defaultExplicit
}

// ResolveProxyURL returns the effective proxy URL string (may be empty = direct).
// explicit overrides env; when explicit is empty, env is used.
func ResolveProxyURL(explicit string) string {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit
	}
	if v := strings.TrimSpace(os.Getenv("XAI_PROXY_OUTBOUND")); v != "" {
		return v
	}
	// Standard env (ALL_PROXY preferred for SOCKS or unified egress).
	for _, key := range []string{"ALL_PROXY", "all_proxy", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

// SchemeKind classifies a proxy URL for transport wiring.
type SchemeKind int

const (
	SchemeNone SchemeKind = iota
	SchemeHTTP
	SchemeSOCKS
)

// ClassifyProxyURL returns the scheme kind for a proxy URL string.
func ClassifyProxyURL(raw string) (SchemeKind, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SchemeNone, nil
	}
	// Allow host:port without scheme → treat as http (common mistake); require scheme for clarity.
	u, err := url.Parse(raw)
	if err != nil {
		return SchemeNone, fmt.Errorf("invalid proxy URL: %w", err)
	}
	if u.Scheme == "" {
		return SchemeNone, fmt.Errorf("proxy URL must include a scheme (http://, https://, socks5://): %q", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return SchemeHTTP, nil
	case "socks5", "socks5h", "socks":
		return SchemeSOCKS, nil
	default:
		return SchemeNone, fmt.Errorf("unsupported proxy scheme %q (use http, https, socks5, socks5h)", u.Scheme)
	}
}

// NewTransport builds an *http.Transport with HTTP and/or SOCKS proxy support.
func NewTransport(opts Options) (*http.Transport, error) {
	proxyURL := ResolveProxyURL(opts.ProxyURL)
	kind, err := ClassifyProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}

	base := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		// Prefer HTTP/1.1 for long-lived streaming proxies: a single stuck HTTP/2
		// stream/connection can stall multiplexed traffic until process restart.
		ForceAttemptHTTP2: false,
		TLSNextProto:      map[string]func(authority string, c *tls.Conn) http.RoundTripper{},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 32,
		MaxConnsPerHost:     64,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		// Bound waits for response headers (body stream may still run long).
		ResponseHeaderTimeout: 120 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	switch kind {
	case SchemeNone:
		// Direct + standard HTTP_PROXY/HTTPS_PROXY via ProxyFromEnvironment.
		// Note: stdlib does not route ALL_PROXY=socks5:// through ProxyFromEnvironment;
		// ResolveProxyURL already surfaces ALL_PROXY, so empty kind means no env proxy either.
		return base, nil

	case SchemeHTTP:
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		// Honor NO_PROXY while forcing this HTTP(S) proxy for other hosts.
		cfg := httpproxy.FromEnvironment()
		cfg.HTTPProxy = u.String()
		cfg.HTTPSProxy = u.String()
		// Keep cfg.NoProxy / CGI from environment.
		pf := cfg.ProxyFunc()
		base.Proxy = func(req *http.Request) (*url.URL, error) {
			return pf(req.URL)
		}
		return base, nil

	case SchemeSOCKS:
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		// Normalize socks:// → socks5:// for FromURL.
		if strings.EqualFold(u.Scheme, "socks") {
			u.Scheme = "socks5"
		}
		dialer, err := proxy.FromURL(u, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("socks proxy: %w", err)
		}
		// HTTP Proxy field must be nil when dialing through SOCKS.
		base.Proxy = nil
		direct := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				host = addr
			}
			// NO_PROXY / no_proxy → dial target directly, skip SOCKS.
			if noProxyBypasses(host) {
				return direct.DialContext(ctx, network, addr)
			}
			if cd, ok := dialer.(proxy.ContextDialer); ok {
				return cd.DialContext(ctx, network, addr)
			}
			return dialer.Dial(network, addr)
		}
		return base, nil
	}

	return base, nil
}

// noProxyBypasses reports whether host matches NO_PROXY / no_proxy list.
func noProxyBypasses(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	raw := os.Getenv("NO_PROXY")
	if raw == "" {
		raw = os.Getenv("no_proxy")
	}
	if raw == "" {
		return false
	}
	for _, p := range strings.Split(raw, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if p == "*" {
			return true
		}
		if strings.HasPrefix(p, ".") {
			// ".example.com" matches host example.com and sub.example.com
			if host == strings.TrimPrefix(p, ".") || strings.HasSuffix(host, p) {
				return true
			}
			continue
		}
		if host == p || strings.HasSuffix(host, "."+p) {
			return true
		}
	}
	return false
}

// NewClient builds an *http.Client using NewTransport.
// When opts.ProxyURL is empty, DefaultProxyURL() (CLI) is used, then env.
func NewClient(opts Options) (*http.Client, error) {
	if strings.TrimSpace(opts.ProxyURL) == "" {
		opts.ProxyURL = defaultExplicit
	}
	tr, err := NewTransport(opts)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout:   opts.Timeout,
		Transport: tr,
	}, nil
}

// MustClient is NewClient that panics only on programmer error; on proxy URL
// errors it returns a direct client with no proxy (and is unused). Prefer NewClient.
func ClientOrDirect(opts Options) *http.Client {
	c, err := NewClient(opts)
	if err != nil {
		// Fall back to direct so existing tests with invalid optional proxy don't crash hard paths.
		return &http.Client{Timeout: opts.Timeout}
	}
	return c
}

// Describe returns a short operator-facing summary of the effective proxy mode.
func Describe(explicit string) string {
	u := ResolveProxyURL(explicit)
	if u == "" {
		return "direct (no outbound proxy)"
	}
	kind, err := ClassifyProxyURL(u)
	if err != nil {
		return "invalid proxy: " + err.Error()
	}
	// Redact userinfo
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword(parsed.User.Username(), "***")
	}
	switch kind {
	case SchemeHTTP:
		return "http(s) proxy " + parsed.String()
	case SchemeSOCKS:
		return "socks proxy " + parsed.String()
	default:
		return "direct"
	}
}
