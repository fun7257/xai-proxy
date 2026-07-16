//go:build unix

package store

import (
	"fmt"
	"os"
	"syscall"
)

// FileLock is an exclusive flock on tokens.json.lock.
type FileLock struct {
	f *os.File
}

// AcquireLock opens/creates the lock file and takes an exclusive flock.
func AcquireLock() (*FileLock, error) {
	if _, err := EnsureHome(); err != nil {
		return nil, err
	}
	path, err := LockPath()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("flock: %w", err)
	}
	return &FileLock{f: f}, nil
}

// Unlock releases the flock.
func (l *FileLock) Unlock() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}
