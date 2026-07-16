package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"xai-proxy/internal/credential"
	"xai-proxy/internal/store"
)

// Server is the local credential-attaching proxy for xAI-native /v1 paths
// (chat + multimodal). OpenAI full-compat only where shapes match.
type Server struct {
	cfg    Config
	http   *http.Server
	host   string
	port   int
}

// Options for Serve.
type Options struct {
	Host              string
	Port              int
	Logger            *slog.Logger
	ClientKeyVerifier string // salted hash from store; never the plaintext secret
}

// NewServer builds a proxy server. manager must be non-nil.
// Upstream HTTP client is built via outbound proxy policy when not injected.
func NewServer(mgr *credential.Manager, opt Options) *Server {
	return NewServerWithUpstream(mgr, nil, opt)
}

// NewServerWithUpstream is like NewServer but uses the given upstream client
// for API forwarding (and share proxy policy with OAuth refresh if desired).
func NewServerWithUpstream(mgr *credential.Manager, upstream *http.Client, opt Options) *Server {
	if opt.Host == "" {
		opt.Host = "127.0.0.1"
	}
	if opt.Port == 0 {
		opt.Port = 7257
	}
	cfg := Config{
		Manager:           mgr,
		Logger:            opt.Logger,
		Upstream:          upstream,
		ClientKeyVerifier: strings.TrimSpace(opt.ClientKeyVerifier),
	}
	s := &Server{cfg: cfg, host: opt.Host, port: opt.Port}
	mux := http.NewServeMux()
	// Liveness / readiness: no client key (probes, container healthcheck).
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	// All API traffic requires the local client API key (verified via hash).
	mux.HandleFunc("/v1/", requireClientAuth(cfg.ClientKeyVerifier, s.cfg.HandleProxyWithRetry))
	mux.HandleFunc("/v1", requireClientAuth(cfg.ClientKeyVerifier, func(w http.ResponseWriter, r *http.Request) {
		writeJSONError(w, http.StatusNotFound, "use /v1/<path> e.g. /v1/chat/completions", "path_not_allowed")
	}))

	s.http = &http.Server{
		Addr:              net.JoinHostPort(opt.Host, fmt.Sprintf("%d", opt.Port)),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// Addr returns the listen address.
func (s *Server) Addr() string {
	return s.http.Addr
}

// ListenAndServe starts the HTTP server (blocking).
func (s *Server) ListenAndServe() error {
	return s.http.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":        "ok",
		"upstream":      "xAI Grok OAuth",
		"authenticated": s.cfg.Manager.IsAuthenticated(),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	st := s.cfg.Manager.Status()
	code := http.StatusOK
	if st.State != store.StatusReady {
		code = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(st)
}

// IsLoopback reports whether host is a loopback address.
func IsLoopback(host string) bool {
	host = strings.TrimSpace(host)
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
