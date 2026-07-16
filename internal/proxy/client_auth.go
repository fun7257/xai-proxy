package proxy

import (
	"net/http"
	"strings"

	"xai-proxy/internal/store"
)

// requireClientAuth wraps a handler and rejects requests without a valid local API key.
// /health and /ready should NOT use this wrapper (liveness probes).
func requireClientAuth(expectedKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expectedKey = strings.TrimSpace(expectedKey)
		if expectedKey == "" {
			writeJSONError(w, http.StatusInternalServerError,
				"server misconfigured: client API key not set",
				"client_auth_misconfigured")
			return
		}
		presented := store.ExtractClientKeyFromRequest(r)
		if !store.EqualClientKey(expectedKey, presented) {
			writeJSONError(w, http.StatusUnauthorized,
				"missing or invalid client API key; use Authorization: Bearer <key> (see xai-proxy key show)",
				"client_unauthorized")
			return
		}
		next(w, r)
	}
}
