package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"xai-proxy/internal/auth"
	"xai-proxy/internal/credential"
	"xai-proxy/internal/outbound"
)

// Default stream timeouts (used when Config fields are zero).
const (
	DefaultStreamIdleTimeout      = 3 * time.Minute
	DefaultStreamWriteIdleTimeout = 60 * time.Second
)

// Stable stream end_reason labels for observability (never log secrets/bodies).
const (
	StreamEndEOF           = "eof"
	StreamEndClientCancel  = "client_cancel"
	StreamEndIdleTimeout   = "idle_timeout"
	StreamEndWriteError    = "write_error"
	StreamEndUpstreamError = "upstream_error"
)

// ErrStreamIdleTimeout is returned when no upstream body bytes arrive within
// StreamIdleTimeout after the response status has already been written
// (streamed=true). Clients therefore see a truncated stream, not a JSON 504;
// the failure is reported via the stream end_reason log field.
// It implements net.Error with Timeout() true so writeUpstreamError would map
// it to HTTP 504 if ever returned before headers (defense in depth only).
var ErrStreamIdleTimeout error = streamIdleTimeoutError{}

type streamIdleTimeoutError struct{}

func (streamIdleTimeoutError) Error() string   { return "upstream stream idle timeout" }
func (streamIdleTimeoutError) Timeout() bool   { return true }
func (streamIdleTimeoutError) Temporary() bool { return true }

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
	// StreamIdleTimeout is the max time between upstream body bytes.
	// 0 = DefaultStreamIdleTimeout (3m); negative = disable (tests).
	StreamIdleTimeout time.Duration
	// StreamWriteIdleTimeout is the max time allowed for a single client write
	// (and Flush). It is set immediately before each write and cleared after,
	// so long healthy gaps between upstream chunks do not expire a sticky
	// connection write deadline. 0 = DefaultStreamWriteIdleTimeout (60s);
	// negative = disable (tests).
	StreamWriteIdleTimeout time.Duration
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
	// ResponseHeaderTimeout lives on outbound pass-through transport.
	client, err := outbound.NewClient(outbound.Options{Timeout: 0})
	if err != nil {
		return outbound.DirectClient(0)
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

// streamIdleTimeoutResolved returns the effective idle-read timeout.
// 0 means disabled.
func (c *Config) streamIdleTimeoutResolved() time.Duration {
	if c.StreamIdleTimeout < 0 {
		return 0
	}
	if c.StreamIdleTimeout == 0 {
		return DefaultStreamIdleTimeout
	}
	return c.StreamIdleTimeout
}

// streamWriteIdleTimeoutResolved returns the effective client write idle window.
// 0 means disabled.
func (c *Config) streamWriteIdleTimeoutResolved() time.Duration {
	if c.StreamWriteIdleTimeout < 0 {
		return 0
	}
	if c.StreamWriteIdleTimeout == 0 {
		return DefaultStreamWriteIdleTimeout
	}
	return c.StreamWriteIdleTimeout
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
	applySSEResponseHeaders(w.Header())
	w.WriteHeader(resp.StatusCode)
	streamed = true

	var flush http.Flusher
	if f, ok := w.(http.Flusher); ok {
		flush = f
	}
	streamStart := time.Now()
	// Pass-through body: no full-buffer requirement; cancel closes upstream on client gone.
	written, reason, copyErr := streamCopy(ctx, w, resp.Body, streamCopyConfig{
		IdleTimeout:      c.streamIdleTimeoutResolved(),
		WriteIdleTimeout: c.streamWriteIdleTimeoutResolved(),
		Flush:            flush,
		ResponseWriter:   w,
	})
	c.logger().Info("proxy stream end",
		"path", rel,
		"status", resp.StatusCode,
		"bytes", written,
		"duration_ms", time.Since(streamStart).Milliseconds(),
		"end_reason", reason,
	)
	if copyErr != nil {
		return resp.StatusCode, true, copyErr
	}
	return resp.StatusCode, true, nil
}

// streamCopyConfig holds resolved streaming options (0 idle/write = feature off).
type streamCopyConfig struct {
	IdleTimeout      time.Duration
	WriteIdleTimeout time.Duration
	Flush            http.Flusher
	ResponseWriter   http.ResponseWriter // optional; enables SetWriteDeadline
}

// streamCopy copies src→dst in chunks without rewriting bytes.
// On ctx cancel it closes src so a blocked Read unblocks (avoids stuck goroutines).
// When IdleTimeout > 0, no upstream bytes for that duration closes src and returns
// ErrStreamIdleTimeout. When WriteIdleTimeout > 0 and ResponseWriter is set,
// a client write deadline is applied only around each write+flush (not across
// upstream Read waits). When Flush is non-nil, each successful write is flushed
// for SSE/chunked clients.
func streamCopy(ctx context.Context, dst io.Writer, src io.ReadCloser, cfg streamCopyConfig) (written int64, reason string, err error) {
	if src == nil {
		return 0, StreamEndEOF, nil
	}
	defer src.Close()

	stop := context.AfterFunc(ctx, func() {
		_ = src.Close()
	})
	defer stop()

	var idleTimedOut atomic.Bool
	var idleTimer *time.Timer
	if cfg.IdleTimeout > 0 {
		idleTimer = time.AfterFunc(cfg.IdleTimeout, func() {
			idleTimedOut.Store(true)
			_ = src.Close()
		})
		defer idleTimer.Stop()
	}

	buf := make([]byte, 32*1024)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			// Reset idle window on upstream bytes. If Stop returns false the
			// AfterFunc already started; idleTimedOut will surface on readErr.
			if idleTimer != nil && !idleTimedOut.Load() && idleTimer.Stop() {
				idleTimer.Reset(cfg.IdleTimeout)
			}
			// Apply write deadline only around client write+flush. Never leave
			// a deadline armed across a long upstream Read: once exceeded,
			// further SetWriteDeadline cannot recover (sticky on HTTP/1 after
			// a failed Write; HTTP/2 kills the stream from the timer alone).
			if cfg.WriteIdleTimeout > 0 {
				setClientWriteDeadline(cfg.ResponseWriter, time.Now().Add(cfg.WriteIdleTimeout))
			}
			if err := writeAll(dst, buf[:n]); err != nil {
				_ = src.Close()
				return written, StreamEndWriteError, err
			}
			written += int64(n)
			if cfg.Flush != nil {
				cfg.Flush.Flush()
			}
			if cfg.WriteIdleTimeout > 0 {
				setClientWriteDeadline(cfg.ResponseWriter, time.Time{})
			}
		}
		if readErr == io.EOF {
			return written, StreamEndEOF, nil
		}
		if readErr != nil {
			if idleTimedOut.Load() {
				return written, StreamEndIdleTimeout, ErrStreamIdleTimeout
			}
			if ctx.Err() != nil {
				return written, StreamEndClientCancel, readErr
			}
			return written, StreamEndUpstreamError, readErr
		}
	}
}

// setClientWriteDeadline sets or clears the ResponseWriter write deadline when
// supported (http.ResponseController). No-op if rw is nil. Errors (including
// ErrNotSupported) are ignored so unsupported wrappers degrade safely.
func setClientWriteDeadline(rw http.ResponseWriter, deadline time.Time) {
	if rw == nil {
		return
	}
	rc := http.NewResponseController(rw)
	_ = rc.SetWriteDeadline(deadline)
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

// ClassifyStreamEndReason maps a stream error to a stable end_reason label.
// Prefer the reason returned by streamCopy on the hot path; this helper is for
// tests and diagnostics when only an error value is available.
func ClassifyStreamEndReason(err error, clientCtx context.Context) string {
	if err == nil {
		return StreamEndEOF
	}
	if errors.Is(err, ErrStreamIdleTimeout) {
		return StreamEndIdleTimeout
	}
	if clientCtx != nil && clientCtx.Err() != nil {
		return StreamEndClientCancel
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return StreamEndClientCancel
	}
	// Client-side write failures: net.OpError with Op=="write", or classic EPIPE wording.
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "write" {
		return StreamEndWriteError
	}
	if strings.Contains(strings.ToLower(err.Error()), "broken pipe") {
		return StreamEndWriteError
	}
	return StreamEndUpstreamError
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
	// Timeouts (ResponseHeaderTimeout, dial, or ErrStreamIdleTimeout if ever
	// returned before headers) → 504. Mid-stream idle already wrote status;
	// those failures never reach this helper.
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		writeJSONError(w, http.StatusGatewayTimeout, "upstream request timed out", "upstream_timeout")
		return
	}
	writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("upstream connection failed: %v", err), "upstream_unreachable")
}
