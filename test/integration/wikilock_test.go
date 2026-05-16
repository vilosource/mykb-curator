//go:build integration

package integration_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/vilosource/mykb-curator/internal/lock"
)

// TestWikiLock_ConcurrentRunsRejected models the v1.0 item-5
// scenario: a long run for a wiki is still going (lock held) when a
// second run for the same wiki starts. The second must be rejected,
// and once the first finishes the wiki is runnable again.
func TestWikiLock_ConcurrentRunsRejected(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "curator.lock")

	// Run A starts.
	runA, err := lock.Acquire(lockPath)
	if err != nil {
		t.Fatalf("run A Acquire: %v", err)
	}

	// Run B starts while A is still active → must be refused.
	if _, err := lock.Acquire(lockPath); !errors.Is(err, lock.ErrLocked) {
		t.Fatalf("run B should be rejected with ErrLocked, got %v", err)
	}

	// Run A finishes.
	if err := runA.Release(); err != nil {
		t.Fatalf("run A Release: %v", err)
	}

	// A later run C now succeeds.
	runC, err := lock.Acquire(lockPath)
	if err != nil {
		t.Fatalf("run C Acquire after A released: %v", err)
	}
	if err := runC.Release(); err != nil {
		t.Fatalf("run C Release: %v", err)
	}
}
