package store

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAndVerifyClientKey(t *testing.T) {
	k1, err := GenerateClientKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(k1) < 20 || !strings.HasPrefix(k1, clientKeyPrefix) {
		t.Fatalf("bad key %q", k1)
	}
	v, err := FormatClientKeyVerifier(k1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(v, k1) {
		t.Fatal("verifier must not embed plaintext key")
	}
	if !strings.HasPrefix(v, "v1$sha256$") {
		t.Fatalf("unexpected format %q", v)
	}
	if !VerifyClientKey(v, k1) {
		t.Fatal("self verify")
	}
	if VerifyClientKey(v, k1+"x") {
		t.Fatal("wrong key must fail")
	}
	if VerifyClientKey(v, "") {
		t.Fatal("empty presented")
	}
	if VerifyClientKey("", k1) {
		t.Fatal("empty verifier")
	}
}

func TestGenerateAndSaveStoresHashOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XAI_PROXY_HOME", dir)

	k1, err := GenerateAndSaveClientKey()
	if err != nil || k1 == "" {
		t.Fatalf("first: k=%q err=%v", k1, err)
	}
	path := filepath.Join(dir, clientKeyFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(raw))
	if strings.Contains(line, k1) {
		t.Fatalf("disk must not contain plaintext: %q", line)
	}
	if !strings.HasPrefix(line, "v1$sha256$") {
		t.Fatalf("unexpected format %q", line)
	}
	v, err := LoadClientKeyVerifier()
	if err != nil || v != line {
		t.Fatalf("load: %q err=%v", v, err)
	}
	if !VerifyClientKey(v, k1) {
		t.Fatal("verify k1")
	}

	k2, err := GenerateAndSaveClientKey()
	if err != nil || k2 == "" || k2 == k1 {
		t.Fatalf("second: k=%q err=%v", k2, err)
	}
	v2, err := LoadClientKeyVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if VerifyClientKey(v2, k1) {
		t.Fatal("old key must fail after overwrite")
	}
	if !VerifyClientKey(v2, k2) {
		t.Fatal("new key must verify")
	}
}

func TestLoadClientKeyVerifierMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XAI_PROXY_HOME", dir)

	v, err := LoadClientKeyVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if v != "" {
		t.Fatalf("want empty, got %q", v)
	}
}

func TestClientKeyConfigured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XAI_PROXY_HOME", dir)

	if ClientKeyConfigured() {
		t.Fatal("expected not configured")
	}
	if _, err := GenerateAndSaveClientKey(); err != nil {
		t.Fatal(err)
	}
	if !ClientKeyConfigured() {
		t.Fatal("expected configured")
	}
}

func TestLoadClientKeyVerifierRejectsLegacy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XAI_PROXY_HOME", dir)
	path := filepath.Join(dir, clientKeyFileName)

	for _, legacy := range []string{
		"sk-xai-deadbeef\n",
		"v2$argon2id$m=65536,t=3,p=1$aa$bb\n",
	} {
		if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadClientKeyVerifier(); err == nil {
			t.Fatalf("expected error for legacy %q", strings.TrimSpace(legacy))
		}
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
