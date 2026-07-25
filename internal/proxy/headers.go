package proxy

import (
	"net/http"
	"strings"
)

// requestDrop: never forward on the outbound API request.
// Includes RFC hop-by-hop headers, Content-Length (proxy sets req.ContentLength
// from the buffered body), and Authorization (replaced with OAuth bearer).
var requestDrop = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailers":            {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"host":                {},
	"content-length":      {},
	"authorization":       {},
}

// responseDrop: hop-by-hop only. Content-Encoding and Content-Length are
// intentionally absent so compressed upstream bodies stay consistent for clients
// (body is stream-copied without rewrite).
var responseDrop = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailers":            {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"host":                {},
}

func copyRequestHeaders(dst, src http.Header) {
	for k, vv := range src {
		lk := strings.ToLower(k)
		if _, drop := requestDrop[lk]; drop {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for k, vv := range src {
		lk := strings.ToLower(k)
		if _, drop := responseDrop[lk]; drop {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// applySSEResponseHeaders adds proxy-friendly defaults for text/event-stream
// responses. Call after copyResponseHeaders and before WriteHeader.
// Non-SSE responses are left unchanged. Existing Cache-Control / X-Accel-Buffering
// values are not clobbered.
func applySSEResponseHeaders(h http.Header) {
	ct := h.Get("Content-Type")
	if !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		return
	}
	if h.Get("Cache-Control") == "" {
		h.Set("Cache-Control", "no-cache")
	}
	if h.Get("X-Accel-Buffering") == "" {
		h.Set("X-Accel-Buffering", "no")
	}
}
