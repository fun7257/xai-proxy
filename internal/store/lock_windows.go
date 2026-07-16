//go:build windows

package store

import (
	"os"
	"time"
)

// FileLock is a best-effort exclusive lock via lock file create on Windows.
type FileLock struct {
	path string
	f    *os.File
}

// AcquireLock creates an exclusive lock file (O_EXCL) with retry.
func AcquireLock() (*FileLock, error) {
	if _, err := EnsureHome(); err != nil {
		return nil, err
	}
	path, err := LockPath()
	if err != nil {
		return nil, err
	}
	var f *os.File
	deadline := time.Now().Add(30 * time.Second)
	for {
		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			return &FileLock{path: path, f: f}, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Unlock releases the lock file.
func (l *FileLock) Unlock() error {
	if l == nil {
		return nil
	}
	var err error
	if l.f != nil {
		err = l.f.Close()
		l.f = nil
	}
	_ = os.Remove(l.path)
	return err
}
