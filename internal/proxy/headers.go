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
