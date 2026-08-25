package outbound

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
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
	if tr.ResponseHeaderTimeout != DefaultResponseHeaderTimeout {
		t.Fatalf("ResponseHeaderTimeout=%v want default %v", tr.ResponseHeaderTimeout, DefaultResponseHeaderTimeout)
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

func TestResponseHeaderTimeoutResolved(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"zero_default", 0, DefaultResponseHeaderTimeout},
		{"custom", 3 * time.Minute, 3 * time.Minute},
		{"negative_disable", -1, 0},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := Options{ResponseHeaderTimeout: tt.in}.responseHeaderTimeoutResolved()
			if got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestNewTransport_ResponseHeaderTimeoutOptions(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("XAI_PROXY_OUTBOUND", "")

	tr, err := NewTransport(Options{ResponseHeaderTimeout: 45 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if tr.ResponseHeaderTimeout != 45*time.Second {
		t.Fatalf("got %v", tr.ResponseHeaderTimeout)
	}

	trOff, err := NewTransport(Options{ResponseHeaderTimeout: -1})
	if err != nil {
		t.Fatal(err)
	}
	if trOff.ResponseHeaderTimeout != 0 {
		t.Fatalf("negative should disable header timeout, got %v", trOff.ResponseHeaderTimeout)
	}
}

func TestNewClient_ResponseHeaderTimeoutCutsSlowHeaders(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("XAI_PROXY_OUTBOUND", "")

	delay := 250 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":1}`))
	}))
	t.Cleanup(srv.Close)

	cut, err := NewClient(Options{Timeout: 0, ResponseHeaderTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cut.Get(srv.URL)
	if err == nil {
		t.Fatal("expected header timeout when upstream thinks longer than ResponseHeaderTimeout")
	}
	if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("err=%v want net.Error Timeout", err)
	}

	okClient, err := NewClient(Options{Timeout: 0, ResponseHeaderTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := okClient.Get(srv.URL)
	if err != nil {
		t.Fatalf("longer header timeout should succeed: %v", err)
	}
	resp.Body.Close()
}

func TestClientOrDirect_InvalidProxyFallsBackToDirect(t *testing.T) {
	c := ClientOrDirect(Options{ProxyURL: "not-a-valid-scheme://x", Timeout: 0})
	tr, ok := c.Transport.(*http.Transport)
	if !ok || !tr.DisableCompression {
		t.Fatal("ClientOrDirect fallback must be DirectClient pass-through transport")
	}
}
