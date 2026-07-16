package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xai-proxy/internal/auth"
	"xai-proxy/internal/credential"
	"xai-proxy/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func makeLongLivedJWT(exp int64) string {
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	p, _ := json.Marshal(map[string]any{"exp": exp})
	body := base64.RawURLEncoding.EncodeToString(p)
	return h + "." + body + ".sig"
}

func setupTokens(t *testing.T) (access string, mgr *credential.Manager) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XAI_PROXY_HOME", dir)
	access = makeLongLivedJWT(time.Now().Add(2 * time.Hour).Unix())
	tok := &store.Tokens{
		Version:       1,
		AccessToken:   access,
		RefreshToken:  "refresh-1",
		TokenType:     "Bearer",
		TokenEndpoint: "https://auth.x.ai/oauth2/token",
		BaseURL:       auth.DefaultAPIBase,
		Status:        store.StatusReady,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := store.Save(tok); err != nil {
		t.Fatal(err)
	}
	return access, credential.NewManager(&http.Client{Timeout: 5 * time.Second})
}

func rewriteToUpstream(upstream *httptest.Server) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req2 := req.Clone(req.Context())
			req2.URL.Scheme = "http"
			req2.URL.Host = strings.TrimPrefix(upstream.URL, "http://")
			req2.RequestURI = ""
			return http.DefaultTransport.RoundTrip(req2)
		}),
	}
}

func newProxyServer(t *testing.T, mgr *credential.Manager, upstream *httptest.Server) *httptest.Server {
	t.Helper()
	// Direct forwarder tests (middleware applied separately in server tests).
	cfg := Config{Manager: mgr, Upstream: rewriteToUpstream(upstream), ClientAPIKey: "sk-xai-test"}
	return httptest.NewServer(http.HandlerFunc(cfg.HandleProxyWithRetry))
}

func TestServer_ClientAuthRequired(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":1}`))
	}))
	defer upstream.Close()
	_, mgr := setupTokens(t)
	const clientKey = "sk-xai-local-test-key-aaaa"
	s := NewServerWithUpstream(mgr, rewriteToUpstream(upstream), Options{
		Host:         "127.0.0.1",
		Port:         0,
		ClientAPIKey: clientKey,
	})
	// Use mux via httptest with ListenAndServe not needed — wrap Handler
	ts := httptest.NewServer(s.http.Handler)
	defer ts.Close()

	// no key
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// wrong key
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	// good key
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", strings.NewReader(`{"model":"grok-4.5","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+clientKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}

	// health open
	resp, err = http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health %d", resp.StatusCode)
	}
}

func TestProxy_ChatForward_StripsClientAuth(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	access, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	payload := `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}]}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer client-garbage")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path=%s", gotPath)
	}
	if gotAuth != "Bearer "+access {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotBody != payload {
		t.Fatalf("body rewritten: %q", gotBody)
	}
}

func TestProxy_ImagesGenerations_Forward(t *testing.T) {
	var gotPath, gotBody, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	access, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	payload := `{"model":"grok-imagine-image","prompt":"a cat"}`
	resp, err := http.Post(srv.URL+"/v1/images/generations", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if gotPath != "/v1/images/generations" {
		t.Fatalf("path=%s", gotPath)
	}
	if gotAuth != "Bearer "+access {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotBody != payload {
		t.Fatalf("body changed")
	}
}

func TestProxy_TTS_Forward_Binary(t *testing.T) {
	var gotPath string
	audio := []byte{0x49, 0x44, 0x33, 0x01} // fake mp3-ish
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(audio)
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/tts", "application/json",
		strings.NewReader(`{"text":"hi","voice_id":"Ara"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if gotPath != "/v1/tts" {
		t.Fatalf("path=%s", gotPath)
	}
	if !bytes.Equal(body, audio) {
		t.Fatalf("binary not passed through: %v", body)
	}
}

func TestProxy_STT_Multipart_PreservesBodyAndContentType(t *testing.T) {
	var gotPath, gotCT string
	var gotFile []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("upstream parse multipart: %v", err)
			w.WriteHeader(400)
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			w.WriteHeader(400)
			return
		}
		defer f.Close()
		gotFile, _ = io.ReadAll(f)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello"}`))
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "a.wav")
	if err != nil {
		t.Fatal(err)
	}
	fileBytes := []byte("RIFF....WAVEfake")
	if _, err := fw.Write(fileBytes); err != nil {
		t.Fatal(err)
	}
	_ = mw.WriteField("language", "en")
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/stt", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s ct_up=%s", resp.StatusCode, b, gotCT)
	}
	if gotPath != "/v1/stt" {
		t.Fatalf("path=%s", gotPath)
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data") {
		t.Fatalf("content-type not multipart: %s", gotCT)
	}
	if !bytes.Equal(gotFile, fileBytes) {
		t.Fatalf("file bytes mutated")
	}
}

func TestProxy_VideoGenerations_AndStatus(t *testing.T) {
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{"request_id":"vid_1"}`))
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/videos/generations", "application/json",
		strings.NewReader(`{"model":"grok-imagine-video","prompt":"waves"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("gen status=%d", resp.StatusCode)
	}

	resp2, err := http.Get(srv.URL + "/v1/videos/vid_1")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("status status=%d", resp2.StatusCode)
	}
	if len(paths) != 2 || paths[0] != "/v1/videos/generations" || paths[1] != "/v1/videos/vid_1" {
		t.Fatalf("paths=%v", paths)
	}
}

func TestProxy_OpenAIAudioPaths_NotShimmed(t *testing.T) {
	// Upstream must never be hit.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream should not be called for OpenAI audio path: %s", r.URL.Path)
		w.WriteHeader(500)
	}))
	defer upstream.Close()

	_, mgr := setupTokens(t)
	srv := newProxyServer(t, mgr, upstream)
	defer srv.Close()

	for _, p := range []string{"/v1/audio/speech", "/v1/audio/transcriptions"} {
		resp, err := http.Post(srv.URL+p, "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status=%d body=%s", p, resp.StatusCode, body)
		}
		var errObj map[string]any
		if err := json.Unmarshal(body, &errObj); err != nil {
			t.Fatal(err)
		}
		e := errObj["error"].(map[string]any)
		if e["code"] != "path_not_allowed" {
			t.Fatalf("code=%v", e["code"])
		}
		msg := e["message"].(string)
		if !strings.Contains(msg, "/v1/tts") && !strings.Contains(msg, "/v1/stt") {
			t.Fatalf("message should point to native paths: %s", msg)
		}
	}
}

func TestProxy_AuthRequired(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XAI_PROXY_HOME", dir)
	mgr := credential.NewManager(nil)
	cfg := Config{Manager: mgr}
	srv := httptest.NewServer(http.HandlerFunc(cfg.HandleProxyWithRetry))
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
}

func TestIsLoopback(t *testing.T) {
	if !IsLoopback("127.0.0.1") || !IsLoopback("localhost") {
		t.Fatal()
	}
	if IsLoopback("0.0.0.0") {
		t.Fatal()
	}
}

func TestMaxBodyBytes_Default(t *testing.T) {
	if MaxBodyBytes < 50*1024*1024 {
		t.Fatalf("media body limit too small: %d", MaxBodyBytes)
	}
}
