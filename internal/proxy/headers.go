package proxy

import (
	"net/http"
	"strings"
)

var hopByHop = map[string]struct{}{
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
	"authorization":       {}, // replaced with OAuth bearer
}

func copyRequestHeaders(dst, src http.Header) {
	for k, vv := range src {
		lk := strings.ToLower(k)
		if _, drop := hopByHop[lk]; drop {
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
		if _, drop := hopByHop[lk]; drop {
			continue
		}
		// Let Go recompute encoding/length when streaming.
		if lk == "content-encoding" || lk == "content-length" {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
