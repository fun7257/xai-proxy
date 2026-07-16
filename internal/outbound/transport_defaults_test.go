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
	// Compile-time-ish use of tls.Conn in type of map value
	var _ map[string]func(string, *tls.Conn) http.RoundTripper = tr.TLSNextProto
}
