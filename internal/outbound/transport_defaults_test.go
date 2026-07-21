package outbound

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"
)

func TestNewTransport_HangMitigations(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("XAI_PROXY_OUTBOUND", "")

	tr, err := NewTransport(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tr.ForceAttemptHTTP2 {
		t.Fatal("HTTP/2 should be disabled to avoid multiplex stalls")
	}
	if tr.TLSNextProto == nil {
		t.Fatal("TLSNextProto should be non-nil empty map to disable h2")
	}
	// Ensure map is empty (no h2 ALPN handlers).
	if len(tr.TLSNextProto) != 0 {
		// Allow only if explicitly empty type
		for k := range tr.TLSNextProto {
			t.Fatalf("unexpected TLSNextProto handler for %q", k)
		}
	}
	if tr.ResponseHeaderTimeout < 30*time.Second {
		t.Fatalf("ResponseHeaderTimeout too low or zero: %v", tr.ResponseHeaderTimeout)
	}
	if tr.MaxIdleConnsPerHost < 8 {
		t.Fatalf("MaxIdleConnsPerHost too small: %d", tr.MaxIdleConnsPerHost)
	}
	if !tr.DisableCompression {
		t.Fatal("DisableCompression must be true for pass-through Content-Encoding")
	}
	// Compile-time-ish use of tls.Conn in type of map value
	var _ map[string]func(string, *tls.Conn) http.RoundTripper = tr.TLSNextProto
}

func TestPassThroughTransport_AndDirectClient(t *testing.T) {
	tr := PassThroughTransport()
	if tr == nil || !tr.DisableCompression {
		t.Fatal("PassThroughTransport must disable compression")
	}
	// Distinct instances so callers cannot mutate a shared global.
	tr2 := PassThroughTransport()
	if tr == tr2 {
		t.Fatal("PassThroughTransport should return a new instance each call")
	}
	c := DirectClient(0)
	ctr, ok := c.Transport.(*http.Transport)
	if !ok || !ctr.DisableCompression {
		t.Fatal("DirectClient must use pass-through transport")
	}
}

func TestClientOrDirect_InvalidProxyFallsBackToDirect(t *testing.T) {
	c := ClientOrDirect(Options{ProxyURL: "not-a-valid-scheme://x", Timeout: 0})
	tr, ok := c.Transport.(*http.Transport)
	if !ok || !tr.DisableCompression {
		t.Fatal("ClientOrDirect fallback must be DirectClient pass-through transport")
	}
}
