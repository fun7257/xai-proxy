package store

import (
	"net/http"
	"strings"
	"testing"
)

func TestGenerateAndEqualClientKey(t *testing.T) {
	k1, err := GenerateClientKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(k1) < 20 {
		t.Fatalf("short key %q", k1)
	}
	if !strings.HasPrefix(k1, clientKeyPrefix) {
		t.Fatalf("prefix %q", k1)
	}
	if !EqualClientKey(k1, k1) {
		t.Fatal("self equal")
	}
	if equalClientKey(k1, k1+"x") {
		t.Fatal("should not equal")
	}
	if equalClientKey("", "x") {
		t.Fatal("empty expected")
	}
}

func equalClientKey(a, b string) bool { return EqualClientKey(a, b) }

func TestLoadOrCreateClientKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XAI_PROXY_HOME", dir)
	t.Setenv("XAI_PROXY_CLIENT_KEY", "")

	k1, created, err := LoadOrCreateClientKey()
	if err != nil || !created || k1 == "" {
		t.Fatalf("k1=%q created=%v err=%v", k1, created, err)
	}
	k2, created2, err := LoadOrCreateClientKey()
	if err != nil || created2 || k2 != k1 {
		t.Fatalf("k2=%q created=%v err=%v", k2, created2, err)
	}
}

func TestLoadOrCreate_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XAI_PROXY_HOME", dir)
	t.Setenv("XAI_PROXY_CLIENT_KEY", "sk-xai-from-env-test-key-0001")
	k, created, err := LoadOrCreateClientKey()
	if err != nil || created || k != "sk-xai-from-env-test-key-0001" {
		t.Fatalf("k=%q created=%v err=%v", k, created, err)
	}
}

func TestExtractClientKeyFromRequest(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer sk-xai-abc")
	if got := ExtractClientKeyFromRequest(r); got != "sk-xai-abc" {
		t.Fatalf("got %q", got)
	}
	r2, _ := http.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("X-Api-Key", "sk-xai-xyz")
	if got := ExtractClientKeyFromRequest(r2); got != "sk-xai-xyz" {
		t.Fatalf("got %q", got)
	}
}
