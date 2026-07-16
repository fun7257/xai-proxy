package auth

import (
	"net/http"
	"time"

	"xai-proxy/internal/outbound"
)

// outboundClient returns a shared-policy HTTP client for OAuth egress
// (discovery, device code, refresh). Honors CLI --proxy and env proxies.
func outboundClient(timeout time.Duration) *http.Client {
	c, err := outbound.NewClient(outbound.Options{Timeout: timeout})
	if err != nil {
		// Last resort: direct client so login surfaces a clear network error later.
		return &http.Client{Timeout: timeout}
	}
	return c
}
