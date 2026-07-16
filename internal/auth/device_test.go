package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestDeviceCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/device/code" {
			// full URL path when using custom base — we hit DeviceCodeURL absolute
			// so this server must match; we override via redirect is hard.
			// Instead test with a custom transport is complex; use absolute URL rewrite.
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "client_id="+ClientID) {
			t.Errorf("missing client_id in body: %s", body)
		}
		if !strings.Contains(string(body), "scope=") {
			t.Errorf("missing scope")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":                "dc",
			"user_code":                  "ABCD-EFGH",
			"verification_uri":           "https://accounts.x.ai/oauth2/device",
			"verification_uri_complete":  "https://accounts.x.ai/oauth2/device?user_code=ABCD-EFGH",
			"expires_in":                 1800,
			"interval":                   1,
		})
	}))
	defer srv.Close()

	// Patch is not possible without changing code; call handler logic via
	// http.Client with Transport RoundTrip rewrite.
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
			req.URL.Path = "/oauth2/device/code"
			return http.DefaultTransport.RoundTrip(req)
		}),
	}
	// RequestDeviceCode uses DeviceCodeURL constant — transport rewrites host.
	dc, err := RequestDeviceCode(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if dc.UserCode != "ABCD-EFGH" {
		t.Fatalf("user_code=%s", dc.UserCode)
	}
}

func TestPollDeviceTokenPendingThenOK(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "authorization_pending",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "acc",
			"refresh_token": "ref",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	// token endpoint must pass ValidateXAIURL — use a host that ends with .x.ai
	// by rewriting only for test: we can't. So use httptest with custom Validate
	// workaround: call poll with endpoint that fails pin...
	// For unit test of poll logic without pin, we test pin separately and
	// use a transport that responds while URL is still https://auth.x.ai/...
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req2 := req.Clone(req.Context())
			req2.URL.Scheme = "http"
			req2.URL.Host = strings.TrimPrefix(srv.URL, "http://")
			req2.RequestURI = ""
			return http.DefaultTransport.RoundTrip(req2)
		}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tr, err := PollDeviceToken(ctx, client, "https://auth.x.ai/oauth2/token", "dc", 30, 1)
	if err != nil {
		t.Fatal(err)
	}
	if tr.AccessToken != "acc" || tr.RefreshToken != "ref" {
		t.Fatalf("%+v", tr)
	}
	if n < 2 {
		t.Fatalf("expected 2 polls, got %d", n)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
