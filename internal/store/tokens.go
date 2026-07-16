package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Status values for stored credentials.
const (
	StatusReady           = "ready"
	StatusReauthRequired  = "reauth_required"
	StatusTierDenied      = "tier_denied"
	StatusUnauthenticated = "unauthenticated"
)

// Tokens is the on-disk credential record.
type Tokens struct {
	Version               int     `json:"version"`
	AccessToken           string  `json:"access_token"`
	RefreshToken          string  `json:"refresh_token"`
	TokenType             string  `json:"token_type"`
	ExpiresIn             int     `json:"expires_in,omitempty"`
	TokenEndpoint         string  `json:"token_endpoint"`
	AuthorizationEndpoint string  `json:"authorization_endpoint,omitempty"`
	BaseURL               string  `json:"base_url,omitempty"`
	UpdatedAt             string  `json:"updated_at"`
	Status                string  `json:"status"`
	LastError             *string `json:"last_error"`
}

// Load reads tokens.json. Returns os.ErrNotExist if missing.
func Load() (*Tokens, error) {
	path, err := TokensPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Tokens
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse tokens.json: %w", err)
	}
	return &t, nil
}

// Save atomically writes tokens.json with mode 0600.
func Save(t *Tokens) error {
	if t == nil {
		return errors.New("nil tokens")
	}
	if _, err := EnsureHome(); err != nil {
		return err
	}
	path, err := TokensPath()
	if err != nil {
		return err
	}
	if t.Version == 0 {
		t.Version = 1
	}
	if t.UpdatedAt == "" {
		t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if t.TokenType == "" {
		t.TokenType = "Bearer"
	}
	if t.Status == "" {
		t.Status = StatusReady
	}

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "tokens.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
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

// Delete removes tokens.json if present.
func Delete() error {
	path, err := TokensPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// HasUsableTokens reports whether access and refresh are non-empty and status allows use.
func HasUsableTokens(t *Tokens) bool {
	if t == nil {
		return false
	}
	if t.Status == StatusReauthRequired {
		return false
	}
	return t.AccessToken != "" && t.RefreshToken != ""
}
