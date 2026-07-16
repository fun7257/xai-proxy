package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestStreamCopy_ByteIdentical drives streamCopy (used by doUpstream) with a
// multi-chunk source and asserts no byte loss or rewrite.
func TestStreamCopy_ByteIdentical(t *testing.T) {
	payload := bytes.Repeat([]byte("abcdefghij"), 4000) // 40 KiB > one buffer
	src := io.NopCloser(bytes.NewReader(payload))
	var dst bytes.Buffer
	if err := streamCopy(context.Background(), &dst, src, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst.Bytes(), payload) {
		t.Fatalf("lost or mutated data: got %d want %d", dst.Len(), len(payload))
	}
}

// TestStreamCopy_ContextCancelUnblocks proves cancel closes the source and
// streamCopy returns promptly (hang mitigation).
func TestStreamCopy_ContextCancelUnblocks(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		var dst bytes.Buffer
		done <- streamCopy(ctx, &dst, pr, nil)
	}()

	// Write one chunk then block on further writes until cancel closes the pipe.
	if _, err := pw.Write([]byte("partial-")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		// Expect error from closed pipe / canceled read, not infinite hang.
		if err == nil {
			t.Fatal("expected error after cancel, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streamCopy hung after context cancel (would freeze proxy)")
	}
	_ = pw.Close()
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
