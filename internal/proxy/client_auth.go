package proxy

import (
	"net/http"
	"strings"

	"xai-proxy/internal/store"
)

// requireClientAuth wraps a handler and rejects requests without a valid local API key.
// verifier is the on-disk salted hash line (never the plaintext secret).
// /health and /ready should NOT use this wrapper (liveness probes).
func requireClientAuth(verifier string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		verifier = strings.TrimSpace(verifier)
		if verifier == "" {
			writeJSONError(w, http.StatusInternalServerError,
				"server misconfigured: client API key not set",
				"client_auth_misconfigured")
			return
		}
		presented := store.ExtractClientKeyFromRequest(r)
		if !store.VerifyClientKey(verifier, presented) {
			writeJSONError(w, http.StatusUnauthorized,
				"missing or invalid client API key; use Authorization: Bearer <key> from xai-proxy generate",
				"client_unauthorized")
			return
		}
		next(w, r)
	}
}
