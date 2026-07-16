//go:build unix

package store

import (
	"strings"
	"testing"
	"time"
)

func TestAcquireLock_TimeoutWhenHeld(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XAI_PROXY_HOME", dir)

	old := lockWaitTimeout
	lockWaitTimeout = 150 * time.Millisecond
	defer func() { lockWaitTimeout = old }()

	held, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer held.Unlock()

	start := time.Now()
	_, err = AcquireLock()
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected lock timeout error while lock is held")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error should mention timeout: %v", err)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("returned too fast (%v); may not have waited", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("lock wait hung too long: %v (would freeze GetBearer)", elapsed)
	}
}
