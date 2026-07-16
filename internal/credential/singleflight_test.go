package credential

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSingleflight_PanicDoesNotBlockWaiters(t *testing.T) {
	var g singleflight
	started := make(chan struct{})
	release := make(chan struct{})

	var leaderErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, leaderErr, _ = g.Do("bearer", func() (any, error) {
			close(started)
			<-release
			panic("refresh boom")
		})
	}()

	<-started

	// Waiters join the same in-flight key.
	type result struct {
		val any
		err error
	}
	waiters := make(chan result, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err, _ := g.Do("bearer", func() (any, error) {
				t.Error("waiter must not re-run fn while flight is active")
				return nil, errors.New("should not run")
			})
			waiters <- result{v, err}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if leaderErr == nil || leaderErr.Error() == "" {
		t.Fatalf("leader should surface panic as error, got %v", leaderErr)
	}
	for i := 0; i < 3; i++ {
		r := <-waiters
		if r.err == nil {
			t.Fatal("waiter expected error from panicked flight")
		}
	}

	// Subsequent flights must work (map entry cleaned up).
	v, err, shared := g.Do("bearer", func() (any, error) {
		return "ok", nil
	})
	if err != nil || v != "ok" {
		t.Fatalf("post-panic flight failed: v=%v err=%v", v, err)
	}
	if shared {
		t.Fatal("expected new flight, not shared")
	}
}
