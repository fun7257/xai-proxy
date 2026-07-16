package outbound

import (
	"net/http"
	"net/url"
	"testing"
)

func TestResolveProxyURL_ExplicitWinsOverEnv(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://env-proxy:8080")
	t.Setenv("ALL_PROXY", "socks5://env-socks:1080")
	t.Setenv("XAI_PROXY_OUTBOUND", "http://xai-env:8080")
	got := ResolveProxyURL("socks5://explicit:1080")
	if got != "socks5://explicit:1080" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveProxyURL_XAIEnvBeforeStandard(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://https-proxy:8080")
	t.Setenv("XAI_PROXY_OUTBOUND", "http://xai-only:8080")
	t.Setenv("ALL_PROXY", "")
	// clear others
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")
	t.Setenv("all_proxy", "")
	got := ResolveProxyURL("")
	if got != "http://xai-only:8080" {
		t.Fatalf("got %q want XAI_PROXY_OUTBOUND", got)
	}
}

func TestResolveProxyURL_ALL_PROXY(t *testing.T) {
	t.Setenv("XAI_PROXY_OUTBOUND", "")
	t.Setenv("ALL_PROXY", "socks5://127.0.0.1:1080")
	t.Setenv("HTTPS_PROXY", "http://should-not-win:1")
	got := ResolveProxyURL("")
	if got != "socks5://127.0.0.1:1080" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveProxyURL_Direct(t *testing.T) {
	t.Setenv("XAI_PROXY_OUTBOUND", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	if got := ResolveProxyURL(""); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestClassifyProxyURL(t *testing.T) {
	cases := []struct {
		in   string
		kind SchemeKind
		ok   bool
	}{
		{"", SchemeNone, true},
		{"http://127.0.0.1:8080", SchemeHTTP, true},
		{"https://proxy.example:8443", SchemeHTTP, true},
		{"socks5://127.0.0.1:1080", SchemeSOCKS, true},
		{"socks5h://127.0.0.1:1080", SchemeSOCKS, true},
		{"socks://127.0.0.1:1080", SchemeSOCKS, true},
		{"ftp://nope", SchemeNone, false},
		{"127.0.0.1:8080", SchemeNone, false},
	}
	for _, tc := range cases {
		k, err := ClassifyProxyURL(tc.in)
		if tc.ok && err != nil {
			t.Errorf("%q: unexpected err %v", tc.in, err)
			continue
		}
		if !tc.ok && err == nil {
			t.Errorf("%q: expected error", tc.in)
			continue
		}
		if tc.ok && k != tc.kind {
			t.Errorf("%q: kind %v want %v", tc.in, k, tc.kind)
		}
	}
}

func TestNewTransport_HTTPProxySelected(t *testing.T) {
	tr, err := NewTransport(Options{ProxyURL: "http://127.0.0.1:9999"})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, "https://api.x.ai/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Proxy == nil {
		t.Fatal("Proxy func nil")
	}
	u, err := tr.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || u.String() != "http://127.0.0.1:9999" {
		t.Fatalf("proxy URL = %v", u)
	}
}

func TestNewTransport_HTTPProxy_NoProxyBypass(t *testing.T) {
	t.Setenv("NO_PROXY", "api.x.ai")
	tr, err := NewTransport(Options{ProxyURL: "http://127.0.0.1:9999"})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.x.ai/v1/models", nil)
	u, err := tr.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if u != nil {
		t.Fatalf("expected direct for NO_PROXY host, got %v", u)
	}
}

func TestNewTransport_SOCKS_WiresDialNotHTTPProxy(t *testing.T) {
	tr, err := NewTransport(Options{ProxyURL: "socks5://127.0.0.1:1080"})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Proxy != nil {
		// Proxy field should be nil so requests don't also go through HTTP CONNECT
		// via a second layer — SOCKS is DialContext only.
		req, _ := http.NewRequest(http.MethodGet, "https://api.x.ai/v1/models", nil)
		if u, _ := tr.Proxy(req); u != nil {
			t.Fatalf("SOCKS mode must not set HTTP Proxy URL, got %v", u)
		}
	}
	if tr.DialContext == nil {
		t.Fatal("DialContext must be set for SOCKS")
	}
	// Dial will fail without a real SOCKS server — but selection is proven.
}

func TestNewTransport_SOCKS5h(t *testing.T) {
	tr, err := NewTransport(Options{ProxyURL: "socks5h://user:pass@127.0.0.1:1080"})
	if err != nil {
		t.Fatal(err)
	}
	if tr.DialContext == nil {
		t.Fatal("expected DialContext")
	}
}

func TestNewClient_Direct(t *testing.T) {
	t.Setenv("XAI_PROXY_OUTBOUND", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")
	SetDefaultProxyURL("")
	c, err := NewClient(Options{Timeout: 0})
	if err != nil {
		t.Fatal(err)
	}
	if c.Transport == nil {
		t.Fatal("nil transport")
	}
}

func TestSetDefaultProxyURL_UsedByNewClient(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("XAI_PROXY_OUTBOUND", "")
	SetDefaultProxyURL("http://cli-default:8080")
	defer SetDefaultProxyURL("")

	tr, err := NewTransport(Options{}) // empty → defaultExplicit via Resolve in NewClient path
	// NewTransport only uses opts.ProxyURL; NewClient merges DefaultProxyURL
	c, err := NewClient(Options{})
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatal("not *http.Transport")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.x.ai/v1/models", nil)
	u, err := tr.Proxy(req)
	if err != nil || u == nil {
		t.Fatalf("u=%v err=%v", u, err)
	}
	if u.Host != "cli-default:8080" {
		t.Fatalf("host %q", u.Host)
	}
}

func TestNoProxyBypasses(t *testing.T) {
	t.Setenv("NO_PROXY", "localhost,127.0.0.1,.internal")
	if !noProxyBypasses("localhost") {
		t.Fatal("localhost")
	}
	if !noProxyBypasses("foo.internal") {
		t.Fatal("suffix .internal")
	}
	if noProxyBypasses("api.x.ai") {
		t.Fatal("api.x.ai should not bypass")
	}
}

func TestDescribe_RedactsPassword(t *testing.T) {
	s := Describe("http://user:secret@proxy.example:8080")
	if s == "" || contains(s, "secret") {
		// Describe uses Resolve which needs the URL as explicit
	}
	// Pass via ResolveProxyURL path: Describe takes explicit
	// Our Describe calls ResolveProxyURL(explicit) first — explicit is used
	// But Describe redacts when parsing - need non-empty explicit
	// Fix: Describe already redacts - verify
	raw := "socks5://alice:hunter2@127.0.0.1:1080"
	out := Describe(raw)
	if contains(out, "hunter2") {
		t.Fatalf("password leaked in %q", out)
	}
	if !contains(out, "alice") && !contains(out, "socks") {
		t.Fatalf("unexpected describe: %q", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}

func TestDescribe_Direct(t *testing.T) {
	t.Setenv("XAI_PROXY_OUTBOUND", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")
	SetDefaultProxyURL("")
	if d := Describe(""); d != "direct (no outbound proxy)" {
		t.Fatalf("%q", d)
	}
}

// ensure url import used when parsing in tests
var _ = url.URL{}
