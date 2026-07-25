package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xai-proxy/internal/outbound"
)

// closeTracker wraps a Reader and records Close calls (for write-error / idle paths).
type closeTracker struct {
	io.Reader
	closed atomic.Bool
	mu     sync.Mutex
	nClose int
}

func (c *closeTracker) Close() error {
	c.mu.Lock()
	c.nClose++
	c.mu.Unlock()
	c.closed.Store(true)
	return nil
}

func (c *closeTracker) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nClose
}

// errWriter fails after maxOK successful bytes (or immediately if maxOK==0).
type errWriter struct {
	n     int
	maxOK int
	err   error
}

func (w *errWriter) Write(p []byte) (int, error) {
	if w.n >= w.maxOK {
		return 0, w.err
	}
	remain := w.maxOK - w.n
	if len(p) > remain {
		w.n += remain
		return remain, w.err
	}
	w.n += len(p)
	return len(p), nil
}

// TestConfig_DefaultUpstreamClient_DisableCompression asserts the production
// default client (no injected Upstream) uses the outbound pass-through contract.
func TestConfig_DefaultUpstreamClient_DisableCompression(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("XAI_PROXY_OUTBOUND", "")

	c := &Config{}
	client := c.upstreamClient()
	if client == nil || client.Transport == nil {
		t.Fatal("expected non-nil client and transport")
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type %T, want *http.Transport from outbound.NewTransport", client.Transport)
	}
	if !tr.DisableCompression {
		t.Fatal("default upstream transport must DisableCompression for pass-through")
	}
}

// TestConfig_UpstreamClient_FallsBackToDirectClient ensures NewClient errors
// do not construct a bare http.Transport in the proxy package.
func TestConfig_UpstreamClient_FallsBackToDirectClient(t *testing.T) {
	// Force NewClient to fail: invalid explicit proxy scheme via env resolution.
	t.Setenv("XAI_PROXY_OUTBOUND", "not-a-valid-scheme://x")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")

	c := &Config{}
	client := c.upstreamClient()
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type %T", client.Transport)
	}
	if !tr.DisableCompression {
		t.Fatal("DirectClient fallback must keep pass-through contract")
	}
}

// TestStreamCopy_ByteIdentical drives streamCopy (used by doUpstream) with a
// multi-chunk source and asserts no byte loss or rewrite.
func TestStreamCopy_ByteIdentical(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{"empty", nil},
		{"small", []byte("hello-sse\n")},
		{"multi_buffer", bytes.Repeat([]byte("abcdefghij"), 4000)}, // 40 KiB > 32KiB buf
		{"binary", append([]byte{0x00, 0xff, 0x10}, bytes.Repeat([]byte{0xab}, 50*1024)...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := io.NopCloser(bytes.NewReader(tt.payload))
			var dst bytes.Buffer
			written, reason, err := streamCopy(context.Background(), &dst, src, streamCopyConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if reason != StreamEndEOF {
				t.Fatalf("reason=%q want %q", reason, StreamEndEOF)
			}
			if written != int64(len(tt.payload)) {
				t.Fatalf("written=%d want %d", written, len(tt.payload))
			}
			if !bytes.Equal(dst.Bytes(), tt.payload) {
				t.Fatalf("lost or mutated data: got %d want %d", dst.Len(), len(tt.payload))
			}
		})
	}
}

// TestStreamCopy_ContextCancelUnblocks proves cancel closes the source and
// streamCopy returns promptly (hang mitigation).
func TestStreamCopy_ContextCancelUnblocks(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		written int64
		reason  string
		err     error
	}
	done := make(chan result, 1)
	go func() {
		var dst bytes.Buffer
		w, reason, err := streamCopy(ctx, &dst, pr, streamCopyConfig{})
		done <- result{w, reason, err}
	}()

	// Write one chunk then block on further writes until cancel closes the pipe.
	if _, err := pw.Write([]byte("partial-")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case res := <-done:
		// Expect error from closed pipe / canceled read, not infinite hang.
		if res.err == nil {
			t.Fatal("expected error after cancel, got nil")
		}
		if res.reason != StreamEndClientCancel {
			t.Fatalf("reason=%q want %q", res.reason, StreamEndClientCancel)
		}
		if res.written != int64(len("partial-")) {
			t.Fatalf("written=%d want %d", res.written, len("partial-"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streamCopy hung after context cancel (would freeze proxy)")
	}
	_ = pw.Close()
}

// TestStreamCopy_IdleTimeout proves a stalled upstream unblocks streamCopy
// within ~1s when StreamIdleTimeout is short (not production 3m default).
func TestStreamCopy_IdleTimeout(t *testing.T) {
	pr, pw := io.Pipe()

	type result struct {
		written int64
		reason  string
		err     error
		elapsed time.Duration
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		var dst bytes.Buffer
		w, reason, err := streamCopy(context.Background(), &dst, pr, streamCopyConfig{
			IdleTimeout: 100 * time.Millisecond,
		})
		done <- result{w, reason, err, time.Since(start)}
	}()

	// One early chunk then stall (never close, never write more).
	if _, err := pw.Write([]byte("tick")); err != nil {
		t.Fatal(err)
	}

	select {
	case res := <-done:
		if !errors.Is(res.err, ErrStreamIdleTimeout) {
			t.Fatalf("err=%v want ErrStreamIdleTimeout", res.err)
		}
		if res.reason != StreamEndIdleTimeout {
			t.Fatalf("reason=%q want %q", res.reason, StreamEndIdleTimeout)
		}
		if res.written != 4 {
			t.Fatalf("written=%d want 4", res.written)
		}
		// Must fire well under 1s (100ms idle + small scheduling slack).
		if res.elapsed > time.Second {
			t.Fatalf("idle timeout took %v; expected within ~1s", res.elapsed)
		}
		if res.elapsed < 80*time.Millisecond {
			t.Fatalf("idle timeout too fast (%v); idle window may not have applied", res.elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streamCopy hung on stalled upstream; idle timeout did not fire")
	}
	_ = pw.Close()
}

// TestStreamCopy_IdleTimeout_ResetsOnBytes ensures bytes arriving within the
// idle window keep the stream alive (timeout is between chunks, not total).
func TestStreamCopy_IdleTimeout_ResetsOnBytes(t *testing.T) {
	pr, pw := io.Pipe()
	idle := 150 * time.Millisecond

	done := make(chan struct {
		written int64
		reason  string
		err     error
	}, 1)
	go func() {
		var dst bytes.Buffer
		w, reason, err := streamCopy(context.Background(), &dst, pr, streamCopyConfig{
			IdleTimeout: idle,
		})
		done <- struct {
			written int64
			reason  string
			err     error
		}{w, reason, err}
	}()

	// Three chunks spaced under the idle window, then clean EOF.
	for i, chunk := range []string{"A", "B", "C"} {
		if _, err := pw.Write([]byte(chunk)); err != nil {
			t.Fatalf("write chunk %d: %v", i, err)
		}
		time.Sleep(idle / 3)
	}
	_ = pw.Close()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("unexpected err: %v", res.err)
		}
		if res.reason != StreamEndEOF {
			t.Fatalf("reason=%q want %q", res.reason, StreamEndEOF)
		}
		if res.written != 3 {
			t.Fatalf("written=%d want 3", res.written)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streamCopy hung during multi-chunk idle-reset test")
	}
}

// TestStreamCopy_WriteDeadline_AllowsSlowUpstreamGaps proves write deadline is
// not left armed across upstream Read waits. Gaps between chunks exceed
// WriteIdleTimeout; a sticky/pre-armed deadline would kill healthy SSE streams.
func TestStreamCopy_WriteDeadline_AllowsSlowUpstreamGaps(t *testing.T) {
	gap := 120 * time.Millisecond
	writeIdle := 40 * time.Millisecond

	pr, pw := io.Pipe()
	type result struct {
		written int64
		reason  string
		err     error
	}
	done := make(chan result, 1)

	// Real HTTP server ResponseWriter supports SetWriteDeadline.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		w.WriteHeader(200)
		if fl != nil {
			fl.Flush()
		}
		written, reason, err := streamCopy(r.Context(), w, pr, streamCopyConfig{
			WriteIdleTimeout: writeIdle,
			Flush:            fl,
			ResponseWriter:   w,
		})
		done <- result{written, reason, err}
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Producer: three chunks with gaps > writeIdle.
	go func() {
		for _, chunk := range []string{"A", "B", "C"} {
			if _, err := pw.Write([]byte(chunk)); err != nil {
				return
			}
			time.Sleep(gap)
		}
		_ = pw.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(body) != "ABC" {
		t.Fatalf("body=%q want ABC (write deadline likely poisoned by inter-chunk gap)", body)
	}

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("streamCopy err=%v reason=%s (write deadline must not fire on slow upstream)", res.err, res.reason)
		}
		if res.reason != StreamEndEOF {
			t.Fatalf("reason=%q want %q", res.reason, StreamEndEOF)
		}
		if res.written != 3 {
			t.Fatalf("written=%d want 3", res.written)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("streamCopy did not finish")
	}
}

// TestProxy_WriteDeadline_SlowUpstreamGaps is the full forward-path regression:
// StreamWriteIdleTimeout shorter than inter-token gaps must not truncate SSE.
func TestProxy_WriteDeadline_SlowUpstreamGaps(t *testing.T) {
	gap := 120 * time.Millisecond
	writeIdle := 40 * time.Millisecond

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, chunk := range []string{"data: 1\n\n", "data: 2\n\n", "data: 3\n\n"} {
			_, _ = w.Write([]byte(chunk))
			fl.Flush()
			time.Sleep(gap)
		}
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	cfg := Config{
		Manager:                mgr,
		Upstream:               rewriteToUpstream(upstream),
		StreamWriteIdleTimeout: writeIdle,
		StreamIdleTimeout:      -1, // isolate write-deadline behavior
	}
	srv := httptest.NewServer(http.HandlerFunc(cfg.HandleProxyWithRetry))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"grok-4.5","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	want := "data: 1\n\ndata: 2\n\ndata: 3\n\n"
	if string(body) != want {
		t.Fatalf("body truncated or mutated:\n got %q\nwant %q", body, want)
	}
}

// TestStreamCopy_WriteErrorClosesUpstream ensures a failed client write closes
// the upstream body (avoids leaking a stuck Read on the source).
func TestStreamCopy_WriteErrorClosesUpstream(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 1024)
	src := &closeTracker{Reader: bytes.NewReader(payload)}
	dst := &errWriter{maxOK: 10, err: errors.New("client write failed")}

	written, reason, err := streamCopy(context.Background(), dst, src, streamCopyConfig{})
	if err == nil {
		t.Fatal("expected write error")
	}
	if !strings.Contains(err.Error(), "client write failed") {
		t.Fatalf("err=%v", err)
	}
	if reason != StreamEndWriteError {
		t.Fatalf("reason=%q want %q", reason, StreamEndWriteError)
	}
	// written only counts fully successful writeAll calls; partial failure → 0.
	if written != 0 {
		t.Fatalf("written=%d want 0 (partial write is not counted)", written)
	}
	if !src.closed.Load() {
		t.Fatal("upstream src was not closed after write error")
	}
	// defer Close + explicit Close on write error may double-close; both OK.
	if src.closeCount() < 1 {
		t.Fatal("Close was never called")
	}
}

// chunkReader returns one pre-defined chunk per Read, then EOF. Used to assert
// flush-once-per-successful-write without depending on kernel buffer sizes.
type chunkReader struct {
	chunks [][]byte
	i      int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.i >= len(r.chunks) {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[r.i])
	r.i++
	return n, nil
}

// flushCounter records Flush calls while writing through to an inner buffer.
type flushCounter struct {
	buf    bytes.Buffer
	nFlush atomic.Int32
}

func (f *flushCounter) Write(p []byte) (int, error) { return f.buf.Write(p) }
func (f *flushCounter) Flush()                      { f.nFlush.Add(1) }

// TestStreamCopy_FlushesOncePerChunk asserts each successful write triggers
// exactly one Flush (SSE/chunked push), not deferred buffering.
func TestStreamCopy_FlushesOncePerChunk(t *testing.T) {
	chunks := [][]byte{
		[]byte("data: 1\n\n"),
		[]byte("data: 2\n\n"),
		[]byte("data: 3\n\n"),
	}
	src := io.NopCloser(&chunkReader{chunks: chunks})
	dst := &flushCounter{}

	written, reason, err := streamCopy(context.Background(), dst, src, streamCopyConfig{
		Flush: dst,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reason != StreamEndEOF {
		t.Fatalf("reason=%q want %q", reason, StreamEndEOF)
	}
	wantBytes := int64(len(chunks[0]) + len(chunks[1]) + len(chunks[2]))
	if written != wantBytes {
		t.Fatalf("written=%d want %d", written, wantBytes)
	}
	if got := int(dst.nFlush.Load()); got != len(chunks) {
		t.Fatalf("Flush called %d times, want %d (once per successful write)", got, len(chunks))
	}
	wantBody := append(append(append([]byte{}, chunks[0]...), chunks[1]...), chunks[2]...)
	if !bytes.Equal(dst.buf.Bytes(), wantBody) {
		t.Fatalf("body mismatch: got %q want %q", dst.buf.Bytes(), wantBody)
	}
}

// TestStreamCopy_NoFlushWhenNil ensures missing Flusher does not panic and
// still delivers bytes (buffered path).
func TestStreamCopy_NoFlushWhenNil(t *testing.T) {
	src := io.NopCloser(strings.NewReader("chunk-a"))
	var dst bytes.Buffer
	written, reason, err := streamCopy(context.Background(), &dst, src, streamCopyConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if reason != StreamEndEOF || written != 7 || dst.String() != "chunk-a" {
		t.Fatalf("written=%d reason=%q body=%q", written, reason, dst.String())
	}
}

// TestStreamIdleTimeoutResolved documents Config semantics for idle read timeout.
func TestStreamIdleTimeoutResolved(t *testing.T) {
	tests := []struct {
		name string
		cfg  time.Duration
		want time.Duration
	}{
		{"zero_default", 0, DefaultStreamIdleTimeout},
		{"negative_disable", -1, 0},
		{"explicit", 200 * time.Millisecond, 200 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{StreamIdleTimeout: tt.cfg}
			if got := c.streamIdleTimeoutResolved(); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

// TestClassifyStreamEndReason covers stable observability labels.
func TestClassifyStreamEndReason(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		err  error
		ctx  context.Context
		want string
	}{
		{"nil_eof", nil, context.Background(), StreamEndEOF},
		{"idle", ErrStreamIdleTimeout, context.Background(), StreamEndIdleTimeout},
		{"client_ctx", errors.New("read closed"), canceled, StreamEndClientCancel},
		{"ctx_canceled_err", context.Canceled, context.Background(), StreamEndClientCancel},
		{"broken_pipe", errors.New("write: broken pipe"), context.Background(), StreamEndWriteError},
		{"op_write", &net.OpError{Op: "write", Net: "tcp", Err: errors.New("connection reset by peer")}, context.Background(), StreamEndWriteError},
		// Read-side reset is upstream/peer failure, not a client write error.
		{"read_connection_reset", errors.New("read: connection reset by peer"), context.Background(), StreamEndUpstreamError},
		// Must not treat arbitrary "write:" substrings as client write failures.
		{"upstream_write_prefix_noise", errors.New("upstream wrote: refused"), context.Background(), StreamEndUpstreamError},
		{"upstream_other", errors.New("connection refused"), context.Background(), StreamEndUpstreamError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyStreamEndReason(tt.err, tt.ctx); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

// TestWriteUpstreamError_TimeoutMaps504 covers net.Error Timeout (including
// ErrStreamIdleTimeout) when headers have not been written yet.
func TestWriteUpstreamError_TimeoutMaps504(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Config{}).writeUpstreamError(rec, ErrStreamIdleTimeout)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status=%d want 504", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "upstream_timeout") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

// TestProxy_GzipPassThrough keeps Content-Encoding and compressed bytes intact.
// Regression: stripping Content-Encoding while forwarding Accept-Encoding left
// clients with gzip bodies and no encoding header (corrupt JSON/SSE).
func TestProxy_GzipPassThrough(t *testing.T) {
	plain := []byte(`{"id":"chatcmpl-gz","choices":[{"message":{"content":"hi"}}]}`)
	var gzipped bytes.Buffer
	zw := gzip.NewWriter(&gzipped)
	if _, err := zw.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	wantBody := gzipped.Bytes()

	var gotAcceptEncoding string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAcceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.Itoa(len(wantBody)))
		_, _ = w.Write(wantBody)
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	// Client advertised gzip — the historical bug path (Transport does not
	// auto-decompress when Accept-Encoding is already set).
	req.Header.Set("Accept-Encoding", "gzip")
	// Observe wire body as the proxy wrote it (no client-side auto-decompress).
	client := outbound.DirectClient(0)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)

	if gotAcceptEncoding != "gzip" {
		t.Fatalf("upstream Accept-Encoding=%q want gzip (must be forwarded)", gotAcceptEncoding)
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("client Content-Encoding=%q want gzip (must not strip)", ce)
	}
	if cl := resp.Header.Get("Content-Length"); cl != strconv.Itoa(len(wantBody)) {
		t.Fatalf("client Content-Length=%q want %d (must match compressed body)", cl, len(wantBody))
	}
	if !bytes.Equal(got, wantBody) {
		t.Fatalf("gzip body not pass-through: got %d bytes want %d", len(got), len(wantBody))
	}
	// Sanity: body is actually gzip of plain.
	gr, err := gzip.NewReader(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("response is not valid gzip: %v", err)
	}
	decoded, err := io.ReadAll(gr)
	_ = gr.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, plain) {
		t.Fatalf("decoded mismatch: %q", decoded)
	}
}

// TestProxy_JSONPassThroughByteIdentical hits the real forward path with an
// injected upstream and asserts request + response bodies are byte-identical.
func TestProxy_JSONPassThroughByteIdentical(t *testing.T) {
	wantReq := []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}]}`)
	wantResp := []byte(`{"id":"chatcmpl-1","choices":[{"message":{"content":"hello"}}],"usage":{"total_tokens":9}}`)

	var gotReqBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotReqBody = append([]byte(nil), b...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(wantResp)
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", bytes.NewReader(wantReq))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer client-key-ignored")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	gotResp, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, gotResp)
	}
	if !bytes.Equal(gotReqBody, wantReq) {
		t.Fatalf("request body rewritten:\n got %q\nwant %q", gotReqBody, wantReq)
	}
	if !bytes.Equal(gotResp, wantResp) {
		t.Fatalf("response body rewritten:\n got %q\nwant %q", gotResp, wantResp)
	}
}

// TestProxy_BinaryPassThroughByteIdentical covers media-shaped bodies (TTS).
func TestProxy_BinaryPassThroughByteIdentical(t *testing.T) {
	wantAudio := []byte{0x00, 0xff, 0x10, 0x20, 0x7f, 0x80, 0xfe, 0x01}
	// pad to >1 buffer chunk path
	wantAudio = append(wantAudio, bytes.Repeat([]byte{0xab}, 40*1024)...)

	var gotReq []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(wantAudio)
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	reqBody := []byte(`{"text":"hi","voice_id":"Ara"}`)
	resp, err := http.Post(srv.URL+"/v1/tts", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(gotReq, reqBody) {
		t.Fatalf("tts request body changed")
	}
	if !bytes.Equal(got, wantAudio) {
		t.Fatalf("tts response lost bytes: got %d want %d", len(got), len(wantAudio))
	}
}

// TestProxy_MultiChunkStream asserts the client receives concatenated upstream
// chunks as they are written (not requiring full upstream completion first for
// the first byte — we still assert full payload integrity).
func TestProxy_MultiChunkStream(t *testing.T) {
	var firstByteSeen atomic.Bool
	chunk1 := []byte("AAA-")
	chunk2 := []byte("BBB-")
	chunk3 := []byte("CCC")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream ResponseWriter is not a Flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write(chunk1)
		fl.Flush()
		time.Sleep(40 * time.Millisecond)
		_, _ = w.Write(chunk2)
		fl.Flush()
		time.Sleep(40 * time.Millisecond)
		_, _ = w.Write(chunk3)
		fl.Flush()
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"grok-4.5","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Read progressively to ensure we can observe data before full EOF.
	buf := make([]byte, 8)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if n > 0 {
		firstByteSeen.Store(true)
	}
	rest, _ := io.ReadAll(resp.Body)
	full := append(buf[:n], rest...)
	want := append(append(chunk1, chunk2...), chunk3...)
	if !bytes.Equal(full, want) {
		t.Fatalf("stream payload mismatch: got %q want %q", full, want)
	}
	if !firstByteSeen.Load() {
		t.Fatal("client never saw streamed bytes")
	}
}

// TestProxy_SSEResponseHeaders asserts SSE enrichment on the real forward path:
// Cache-Control: no-cache and X-Accel-Buffering: no for text/event-stream.
func TestProxy_SSEResponseHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Intentionally omit Cache-Control / X-Accel-Buffering — proxy fills them.
		w.WriteHeader(200)
		_, _ = w.Write([]byte("data: hi\n\n"))
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"grok-4.5","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type=%q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control=%q want no-cache", cc)
	}
	if xa := resp.Header.Get("X-Accel-Buffering"); xa != "no" {
		t.Fatalf("X-Accel-Buffering=%q want no", xa)
	}
	if !bytes.Equal(body, []byte("data: hi\n\n")) {
		t.Fatalf("body mutated: %q", body)
	}
}

// TestProxy_NonSSE_NoAccelBufferingForced ensures JSON (and other non-SSE)
// responses do not get proxy-injected X-Accel-Buffering / Cache-Control defaults.
func TestProxy_NonSSE_NoAccelBufferingForced(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"grok-4.5","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if xa := resp.Header.Get("X-Accel-Buffering"); xa != "" {
		t.Fatalf("non-SSE must not force X-Accel-Buffering; got %q", xa)
	}
	// Cache-Control may be absent or set by upstream only — we never inject for JSON.
	// (upstream did not set it; assert proxy left it empty.)
	if cc := resp.Header.Get("Cache-Control"); cc != "" {
		t.Fatalf("non-SSE must not force Cache-Control; got %q", cc)
	}
}

// TestProxy_ClientCancelDuringSlowUpstream exercises the real forward path:
// client cancels while upstream holds the body open; proxy must finish promptly.
func TestProxy_ClientCancelDuringSlowUpstream(t *testing.T) {
	var upstreamEntered sync.WaitGroup
	upstreamEntered.Add(1)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		w.WriteHeader(200)
		_, _ = w.Write([]byte("x"))
		fl.Flush()
		upstreamEntered.Done()
		// Stay open until client/proxy cancels the request context.
		select {
		case <-r.Context().Done():
			return
		case <-time.After(15 * time.Second):
			t.Error("upstream request context never cancelled")
		}
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"grok-4.5","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")

	errCh := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		errCh <- nil
	}()

	upstreamEntered.Wait()
	cancel()

	select {
	case <-errCh:
		// Client-side error or completion is fine; must not hang the test.
	case <-time.After(3 * time.Second):
		t.Fatal("client request hung after cancel; proxy likely stuck reading upstream")
	}
}
