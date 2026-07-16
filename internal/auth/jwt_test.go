package auth

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func makeJWT(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(map[string]any{"exp": exp})
	body := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + body + ".sig"
}

func TestAccessTokenIsExpiring(t *testing.T) {
	// far future
	tok := makeJWT(time.Now().Add(2 * time.Hour).Unix())
	if AccessTokenIsExpiring(tok, 120) {
		t.Fatal("should not be expiring")
	}
	// past
	tok = makeJWT(time.Now().Add(-time.Minute).Unix())
	if !AccessTokenIsExpiring(tok, 0) {
		t.Fatal("should be expired")
	}
	// opaque
	if AccessTokenIsExpiring("not-a-jwt", 3600) {
		t.Fatal("opaque should not report expiring")
	}
}

func TestProactiveRefreshSkewSeconds(t *testing.T) {
	// long remaining → max skew
	tok := makeJWT(time.Now().Add(2 * time.Hour).Unix())
	if got := ProactiveRefreshSkewSeconds(tok); got != MaxRefreshSkewSeconds {
		t.Fatalf("long jwt skew=%d want %d", got, MaxRefreshSkewSeconds)
	}
	// short remaining (e.g. 10 min) → short skew
	tok = makeJWT(time.Now().Add(10 * time.Minute).Unix())
	if got := ProactiveRefreshSkewSeconds(tok); got != ShortJWTSkewSeconds {
		t.Fatalf("short jwt skew=%d want %d", got, ShortJWTSkewSeconds)
	}
}
