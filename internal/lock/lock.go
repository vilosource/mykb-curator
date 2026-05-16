// Package lock provides a per-wiki advisory file lock so two curator
// runs can never operate on the same wiki concurrently (DESIGN.md
// §17 v1.0: "per-wiki lock + atomicity hardening").
//
// Without this, two overlapping runs (e.g. a slow cron run still
// going when the next fires) would both render + upsert the same
// pages, racing each other's bot revisions and corrupting the
// run-state cache's lastBotRevID bookkeeping.
//
// Mechanism: flock(2) LOCK_EX|LOCK_NB on a lockfile. The kernel
// releases the lock automatically if the process dies, so there is
// no stale-lock problem (no PID files, no TTL heuristics). Acquire
// is non-blocking: a second run fails fast with ErrLocked rather
// than queueing — the right behaviour for a scheduled tool.
//
// Unix-only by design; the curator runs on Linux (CI + deployment).
package lock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrLocked is returned by Acquire when another holder owns the lock.
var ErrLocked = errors.New("lock: already held by another run")

// FileLock is an acquired advisory lock. Release exactly once.
type FileLock struct {
	path string
	f    *os.File
}

// Acquire takes an exclusive non-blocking lock on path, creating the
// lockfile if needed. Returns ErrLocked if another process (or
// another open file description in this one) holds it.
func Acquire(path string) (*FileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock: open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (%s)", ErrLocked, path)
		}
		return nil, fmt.Errorf("lock: flock %s: %w", path, err)
	}
	return &FileLock{path: path, f: f}, nil
}

// Release unlocks and closes the lockfile. Safe to call once;
// subsequent calls are no-ops.
func (l *FileLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	ferr := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	cerr := l.f.Close()
	l.f = nil
	if ferr != nil {
		return fmt.Errorf("lock: unlock %s: %w", l.path, ferr)
	}
	if cerr != nil {
		return fmt.Errorf("lock: close %s: %w", l.path, cerr)
	}
	return nil
}
