//go:build unix

package store

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// lockWaitTimeout bounds how long we block on flock (another hung process).
// Overridable in tests.
var lockWaitTimeout = 45 * time.Second

// FileLock is an exclusive flock on tokens.json.lock.
type FileLock struct {
	f *os.File
}

// AcquireLock opens/creates the lock file and takes an exclusive flock.
// Uses non-blocking flock with retry so a stuck holder cannot freeze the proxy forever.
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
	deadline := time.Now().Add(lockWaitTimeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &FileLock{f: f}, nil
		}
		// EWOULDBLOCK / EAGAIN: held by another process or goroutine path.
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			f.Close()
			return nil, fmt.Errorf("flock: %w", err)
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("token lock timeout after %s (another process may be stuck holding %s)", lockWaitTimeout, path)
		}
		time.Sleep(25 * time.Millisecond)
	}
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
