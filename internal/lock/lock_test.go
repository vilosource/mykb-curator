package lock_test

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/vilosource/mykb-curator/internal/lock"
)

func TestAcquire_Release_RoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "wiki.lock")
	l, err := lock.Acquire(p)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	// After release, a fresh acquire must succeed.
	l2, err := lock.Acquire(p)
	if err != nil {
		t.Fatalf("re-Acquire after Release: %v", err)
	}
	_ = l2.Release()
}

func TestAcquire_SecondIsBlocked(t *testing.T) {
	p := filepath.Join(t.TempDir(), "wiki.lock")
	l, err := lock.Acquire(p)
	if err != nil {
		t.Fatalf("Acquire #1: %v", err)
	}
	defer func() { _ = l.Release() }()

	_, err = lock.Acquire(p)
	if !errors.Is(err, lock.ErrLocked) {
		t.Fatalf("second Acquire err = %v, want ErrLocked", err)
	}
}

func TestAcquire_DifferentPathsIndependent(t *testing.T) {
	dir := t.TempDir()
	a, err := lock.Acquire(filepath.Join(dir, "a.lock"))
	if err != nil {
		t.Fatalf("Acquire a: %v", err)
	}
	defer func() { _ = a.Release() }()
	b, err := lock.Acquire(filepath.Join(dir, "b.lock"))
	if err != nil {
		t.Fatalf("Acquire b (independent path) should succeed: %v", err)
	}
	_ = b.Release()
}

// TestConcurrentAcquire_ExactlyOneWins is the concurrency-correctness
// assertion behind v1.0 item 5: many racing "runs" against one wiki
// lock, exactly one holds it at a time.
func TestConcurrentAcquire_ExactlyOneWins(t *testing.T) {
	p := filepath.Join(t.TempDir(), "wiki.lock")
	const goroutines = 16

	var (
		wins    atomic.Int32
		inside  atomic.Int32
		maxSeen atomic.Int32
		wg      sync.WaitGroup
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := lock.Acquire(p)
			if errors.Is(err, lock.ErrLocked) {
				return
			}
			if err != nil {
				t.Errorf("unexpected Acquire error: %v", err)
				return
			}
			wins.Add(1)
			n := inside.Add(1)
			for {
				m := maxSeen.Load()
				if n <= m || maxSeen.CompareAndSwap(m, n) {
					break
				}
			}
			inside.Add(-1)
			_ = l.Release()
		}()
	}
	wg.Wait()

	if wins.Load() == 0 {
		t.Fatalf("no goroutine acquired the lock")
	}
	if got := maxSeen.Load(); got > 1 {
		t.Fatalf("mutual exclusion violated: %d holders observed simultaneously", got)
	}
}
