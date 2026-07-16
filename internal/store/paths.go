package store

import (
	"os"
	"path/filepath"
)

const (
	defaultDirName  = ".xai-proxy"
	tokensFileName  = "tokens.json"
	lockFileName    = "tokens.json.lock"
	envHomeOverride = "XAI_PROXY_HOME"
)

// HomeDir returns the config directory (~/.xai-proxy or XAI_PROXY_HOME).
func HomeDir() (string, error) {
	if v := os.Getenv(envHomeOverride); v != "" {
		return filepath.Clean(v), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultDirName), nil
}

// TokensPath is the path to tokens.json.
func TokensPath() (string, error) {
	dir, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tokensFileName), nil
}

// LockPath is the path to the flock file.
func LockPath() (string, error) {
	dir, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, lockFileName), nil
}

// EnsureHome creates the home directory with 0700 permissions.
func EnsureHome() (string, error) {
	dir, err := HomeDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
