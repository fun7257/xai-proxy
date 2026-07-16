package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRefreshRotatesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "grant_type=refresh_token") {
			t.Errorf("body=%s", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"token_type":    "Bearer",
		})
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req2 := req.Clone(req.Context())
			req2.URL.Scheme = "http"
			req2.URL.Host = strings.TrimPrefix(srv.URL, "http://")
			req2.RequestURI = ""
			return http.DefaultTransport.RoundTrip(req2)
		}),
	}
	tr, err := Refresh(context.Background(), client, "https://auth.x.ai/oauth2/token", "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if tr.AccessToken != "new-access" || tr.RefreshToken != "new-refresh" {
		t.Fatalf("%+v", tr)
	}
}

func TestRefreshKeepsOldRefreshWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access",
			"token_type":   "Bearer",
		})
	}))
	defer srv.Close()
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req2 := req.Clone(req.Context())
			req2.URL.Scheme = "http"
			req2.URL.Host = strings.TrimPrefix(srv.URL, "http://")
			req2.RequestURI = ""
			return http.DefaultTransport.RoundTrip(req2)
		}),
	}
	tr, err := Refresh(context.Background(), client, "https://auth.x.ai/oauth2/token", "keep-me")
	if err != nil {
		t.Fatal(err)
	}
	if tr.RefreshToken != "keep-me" {
		t.Fatalf("refresh=%s", tr.RefreshToken)
	}
}

func TestRefresh403TierDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"permission denied"}`))
	}))
	defer srv.Close()
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req2 := req.Clone(req.Context())
			req2.URL.Scheme = "http"
			req2.URL.Host = strings.TrimPrefix(srv.URL, "http://")
			req2.RequestURI = ""
			return http.DefaultTransport.RoundTrip(req2)
		}),
	}
	_, err := Refresh(context.Background(), client, "https://auth.x.ai/oauth2/token", "r")
	if !IsTierDenied(err) {
		t.Fatalf("want tier denied, got %v", err)
	}
	if IsTerminalRefresh(err) {
		t.Fatal("tier denied should not be terminal relogin-style wipe trigger only — IsTerminalRefresh false")
	}
}

func TestRefresh400Terminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req2 := req.Clone(req.Context())
			req2.URL.Scheme = "http"
			req2.URL.Host = strings.TrimPrefix(srv.URL, "http://")
			req2.RequestURI = ""
			return http.DefaultTransport.RoundTrip(req2)
		}),
	}
	_, err := Refresh(context.Background(), client, "https://auth.x.ai/oauth2/token", "r")
	if !IsTerminalRefresh(err) {
		t.Fatalf("want terminal, got %v", err)
	}
}
