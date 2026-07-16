package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"xai-proxy/internal/auth"
	"xai-proxy/internal/credential"
	"xai-proxy/internal/outbound"
)

// Config for the reverse proxy.
type Config struct {
	Manager *credential.Manager
	// ClientKeyVerifier is the salted hash line from store (never plaintext).
	// Required for production serve; empty rejects all /v1 requests.
	ClientKeyVerifier string
	// AllowedPaths, if non-nil, overrides PathAllowed with an exact map (tests only).
	AllowedPaths map[string]struct{}
	// MaxBodyBytes overrides MaxBodyBytes default when > 0.
	MaxBodyBytes int64
	Logger       *slog.Logger
	// Upstream client — no total Timeout so streams can run long.
	Upstream *http.Client
}

func (c *Config) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

func (c *Config) upstreamClient() *http.Client {
	if c.Upstream != nil {
		return c.Upstream
	}
	// Streaming API: no client-level total Timeout (body may stream for a long time).
	// ResponseHeaderTimeout is set on the transport in outbound.NewTransport.
	client, err := outbound.NewClient(outbound.Options{Timeout: 0})
	if err != nil {
		return &http.Client{Timeout: 0}
	}
	return client
}

func (c *Config) bodyLimit() int64 {
	if c.MaxBodyBytes > 0 {
		return c.MaxBodyBytes
	}
	return MaxBodyBytes
}

func (c *Config) isAllowed(rel string) bool {
	if c.AllowedPaths != nil {
		return pathAllowed(rel, c.AllowedPaths)
	}
	return PathAllowed(rel)
}

// HandleProxyWithRetry implements credential attach + one 401 retry.
// Body is passed through unchanged (JSON or multipart); only Authorization is replaced.
func (c *Config) HandleProxyWithRetry(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rel := strings.TrimPrefix(r.URL.Path, "/v1")
	if rel == "" {
		rel = "/"
	}
	rel = normalizePath(rel)

	if !c.isAllowed(rel) {
		msg := fmt.Sprintf(
			"Path /v1%s is not forwarded. %s. OpenAI /audio/* is not shimmed — use xAI native /v1/tts and /v1/stt.",
			rel, AllowedPathSummary(),
		)
		if IsOpenAIAudioShimRejected(rel) {
			msg = fmt.Sprintf(
				"Path /v1%s is OpenAI-only and is not mapped. Use xAI native /v1/tts (TTS) or /v1/stt (STT).",
				rel,
			)
		}
		writeJSONError(w, http.StatusNotFound, msg, "path_not_allowed")
		return
	}

	// Read body once so 401 refresh can retry. Preserves multipart bytes as-is.
	limit := c.bodyLimit()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("request body too large or unreadable (max %d bytes)", limit),
			"body_error")
		return
	}

	bearer, baseURL, err := c.Manager.GetBearer(r.Context())
	if err != nil {
		c.writeAuthError(w, err)
		return
	}

	status, streamed, err := c.doUpstream(r.Context(), w, r, rel, body, bearer, baseURL, true)
	if err != nil && !streamed {
		c.writeUpstreamError(w, err)
		return
	}
	if status == http.StatusUnauthorized && !streamed {
		bearer, baseURL, err = c.Manager.ForceRefresh(r.Context())
		if err != nil {
			c.writeAuthError(w, err)
			return
		}
		status, streamed, err = c.doUpstream(r.Context(), w, r, rel, body, bearer, baseURL, false)
		if err != nil && !streamed {
			c.writeUpstreamError(w, err)
			return
		}
	}

	c.logger().Info("proxy request",
		"method", r.Method,
		"path", rel,
		"upstream_status", status,
		"latency_ms", time.Since(start).Milliseconds(),
		"body_bytes", len(body),
	)
}

// doUpstream performs one upstream call. If peekAuth is true and status is 401,
// it does not write the body (caller may retry). Otherwise streams the response
// (JSON, binary audio, SSE).
func (c *Config) doUpstream(ctx context.Context, w http.ResponseWriter, r *http.Request, rel string, body []byte, bearer, baseURL string, peekAuth bool) (status int, streamed bool, err error) {
	// base_url is https://api.x.ai/v1; rel is /chat/completions → full upstream path.
	upURL := strings.TrimRight(baseURL, "/") + rel
	if r.URL.RawQuery != "" {
		upURL += "?" + r.URL.RawQuery
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, upURL, bodyReader)
	if err != nil {
		return 0, false, err
	}
	// Copy client headers except hop-by-hop and Authorization (replaced below).
	// Multipart Content-Type (with boundary) is preserved as-is — no body rewrite.
	copyRequestHeaders(req.Header, r.Header)
	req.Header.Set("Authorization", "Bearer "+bearer)
	// Only default Content-Type when client sent none and body is non-empty.
	// Never force application/json over multipart/form-data.
	if len(body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.ContentLength = int64(len(body))

	resp, err := c.upstreamClient().Do(req)
	if err != nil {
		return 0, false, err
	}

	if peekAuth && resp.StatusCode == http.StatusUnauthorized {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode, false, nil
	}

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	streamed = true

	var flush http.Flusher
	if f, ok := w.(http.Flusher); ok {
		flush = f
	}
	// Pass-through body: no full-buffer requirement; cancel closes upstream on client gone.
	if err := streamCopy(ctx, w, resp.Body, flush); err != nil {
		return resp.StatusCode, true, err
	}
	return resp.StatusCode, true, nil
}

// streamCopy copies src→dst in chunks without rewriting bytes.
// On ctx cancel it closes src so a blocked Read unblocks (avoids stuck goroutines).
// When flush is non-nil, each successful write is flushed for SSE/chunked clients.
func streamCopy(ctx context.Context, dst io.Writer, src io.ReadCloser, flush http.Flusher) error {
	if src == nil {
		return nil
	}
	defer src.Close()

	stop := context.AfterFunc(ctx, func() {
		_ = src.Close()
	})
	defer stop()

	buf := make([]byte, 32*1024)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if err := writeAll(dst, buf[:n]); err != nil {
				_ = src.Close()
				return err
			}
			if flush != nil {
				flush.Flush()
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// writeAll writes p fully (handles short writes) without modifying content.
func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n > 0 {
			p = p[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (c *Config) writeAuthError(w http.ResponseWriter, err error) {
	if auth.IsTierDenied(err) {
		writeJSONError(w, http.StatusForbidden, err.Error(), "upstream_tier_denied")
		return
	}
	if e, ok := err.(*auth.Error); ok && e.ReloginRequired {
		writeJSONError(w, http.StatusServiceUnavailable, err.Error(), "auth_required")
		return
	}
	writeJSONError(w, http.StatusBadGateway, err.Error(), "upstream_auth_failed")
}

func (c *Config) writeUpstreamError(w http.ResponseWriter, err error) {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		writeJSONError(w, http.StatusGatewayTimeout, "upstream request timed out", "upstream_timeout")
		return
	}
	writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("upstream connection failed: %v", err), "upstream_unreachable")
}
