package store

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	clientKeyFileName = "client_key"
	clientKeyPrefix   = "sk-xai-"
	clientKeyBytes    = 32 // 256-bit
)

// ClientKeyPath returns the path to the local client API key file.
func ClientKeyPath() (string, error) {
	dir, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, clientKeyFileName), nil
}

// GenerateClientKey returns a new random client API key (sk-xai- + hex).
func GenerateClientKey() (string, error) {
	b := make([]byte, clientKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return clientKeyPrefix + hex.EncodeToString(b), nil
}

// LoadClientKey reads the client key file. Returns "" if missing.
func LoadClientKey() (string, error) {
	path, err := ClientKeyPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// SaveClientKey writes the client key with mode 0600 (atomic).
func SaveClientKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("empty client key")
	}
	if _, err := EnsureHome(); err != nil {
		return err
	}
	path, err := ClientKeyPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "client_key.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(key + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// LoadOrCreateClientKey returns the existing key or generates and saves a new one.
// created is true when a new key was written.
func LoadOrCreateClientKey() (key string, created bool, err error) {
	// Explicit env override never written to disk (operator injects secret).
	if env := strings.TrimSpace(os.Getenv("XAI_PROXY_CLIENT_KEY")); env != "" {
		return env, false, nil
	}
	key, err = LoadClientKey()
	if err != nil {
		return "", false, err
	}
	if key != "" {
		return key, false, nil
	}
	key, err = GenerateClientKey()
	if err != nil {
		return "", false, err
	}
	if err := SaveClientKey(key); err != nil {
		return "", false, err
	}
	return key, true, nil
}

// EqualClientKey compares presented vs expected in constant time.
// Empty expected means no valid key configured.
func EqualClientKey(expected, presented string) bool {
	expected = strings.TrimSpace(expected)
	presented = strings.TrimSpace(presented)
	if expected == "" || presented == "" {
		return false
	}
	// subtle.ConstantTimeCompare requires equal length
	a := []byte(expected)
	b := []byte(presented)
	if len(a) != len(b) {
		// Compare against dummy of same length to reduce length leak timing a bit
		_ = subtle.ConstantTimeCompare(a, a)
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

// ExtractClientKeyFromRequest pulls the key from common client headers.
//
// Accepted forms:
//   - Authorization: Bearer <key>
//   - Authorization: <key>
//   - X-Api-Key: <key>
//   - Api-Key: <key>
func ExtractClientKeyFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := strings.TrimSpace(r.Header.Get("X-Api-Key")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("Api-Key")); v != "" {
		return v
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if authz == "" {
		return ""
	}
	const bearer = "bearer "
	if len(authz) > len(bearer) && strings.EqualFold(authz[:len(bearer)], bearer) {
		return strings.TrimSpace(authz[len(bearer):])
	}
	return authz
}

// FormatClientKeyPath returns a display path or error string for messages.
func FormatClientKeyPath() string {
	p, err := ClientKeyPath()
	if err != nil {
		return "(unknown)"
	}
	return p
}

// EnsureClientKeyConfigured is a helper error message.
func EnsureClientKeyConfigured() error {
	k, _, err := LoadOrCreateClientKey()
	if err != nil {
		return err
	}
	if k == "" {
		return fmt.Errorf("client API key missing")
	}
	return nil
}
