package store

import (
	"crypto/rand"
	"crypto/sha256"
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
	clientKeyFileName  = "client_key"
	clientKeyPrefix    = "sk-xai-"
	clientKeyBytes     = 32 // 256-bit secret
	clientKeySaltBytes = 16
	// On-disk verifier: v1$sha256$<salt_hex>$<sha256(salt||key)_hex>
	// Never stores the plaintext API key. Suitable for high-entropy random keys.
	clientKeyVerifierVersion = "v1"
	clientKeyHashAlg         = "sha256"
)

// ClientKeyPath returns the path to the client key *verifier* file (hash only).
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

// hashClientKey returns SHA-256(salt || key). Irreversible for high-entropy keys.
func hashClientKey(salt []byte, key string) []byte {
	h := sha256.New()
	_, _ = h.Write(salt)
	_, _ = h.Write([]byte(key))
	return h.Sum(nil)
}

// FormatClientKeyVerifier builds the on-disk verifier line for key (with random salt).
func FormatClientKeyVerifier(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("empty client key")
	}
	salt := make([]byte, clientKeySaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := hashClientKey(salt, key)
	return fmt.Sprintf("%s$%s$%s$%s",
		clientKeyVerifierVersion,
		clientKeyHashAlg,
		hex.EncodeToString(salt),
		hex.EncodeToString(sum),
	), nil
}

// parseClientKeyVerifier parses v1$sha256$salt$hash.
// Rejects plaintext and other formats (re-run generate).
func parseClientKeyVerifier(line string) (salt, wantHash []byte, err error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil, errors.New("empty verifier")
	}
	parts := strings.Split(line, "$")
	if len(parts) != 4 {
		return nil, nil, errors.New("invalid client key verifier format (run xai-proxy generate)")
	}
	if parts[0] != clientKeyVerifierVersion || parts[1] != clientKeyHashAlg {
		return nil, nil, fmt.Errorf("unsupported client key verifier %s$%s (run xai-proxy generate)", parts[0], parts[1])
	}
	salt, err = hex.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return nil, nil, errors.New("invalid client key verifier salt")
	}
	wantHash, err = hex.DecodeString(parts[3])
	if err != nil || len(wantHash) != sha256.Size {
		return nil, nil, errors.New("invalid client key verifier hash")
	}
	return salt, wantHash, nil
}

// LoadClientKeyVerifier reads the on-disk hash verifier. Returns "" if missing.
// Does not return the plaintext secret (it is never stored).
func LoadClientKeyVerifier() (string, error) {
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
	line := strings.TrimSpace(string(data))
	if line == "" {
		return "", nil
	}
	if _, _, err := parseClientKeyVerifier(line); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return line, nil
}

// SaveClientKeyVerifier writes the verifier line with mode 0600 (atomic overwrite).
func SaveClientKeyVerifier(verifier string) error {
	verifier = strings.TrimSpace(verifier)
	if _, _, err := parseClientKeyVerifier(verifier); err != nil {
		return err
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

	if _, err := tmp.WriteString(verifier + "\n"); err != nil {
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

// GenerateAndSaveClientKey mints a new key, stores only a salted SHA-256 hash on disk,
// and returns the plaintext once. Each call overwrites the previous verifier.
func GenerateAndSaveClientKey() (plaintext string, err error) {
	key, err := GenerateClientKey()
	if err != nil {
		return "", err
	}
	verifier, err := FormatClientKeyVerifier(key)
	if err != nil {
		return "", err
	}
	if err := SaveClientKeyVerifier(verifier); err != nil {
		return "", err
	}
	return key, nil
}

// VerifyClientKey checks presented plaintext against a stored verifier line.
// Uses constant-time compare on the hash digests.
func VerifyClientKey(verifier, presented string) bool {
	salt, want, err := parseClientKeyVerifier(verifier)
	if err != nil {
		return false
	}
	presented = strings.TrimSpace(presented)
	if presented == "" {
		return false
	}
	got := hashClientKey(salt, presented)
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
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

// ClientKeyConfigured reports whether a valid client-key verifier is on disk.
// Never includes the plaintext key (it is not stored).
func ClientKeyConfigured() bool {
	v, err := LoadClientKeyVerifier()
	return err == nil && v != ""
}
