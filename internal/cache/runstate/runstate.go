// Package runstate persists per-spec state between curator runs.
//
// State stored per spec:
//   - LastBotRevID: the revision ID the curator wrote on its last run
//   - LastKBCommit: kb commit the last render was against
//   - LastRenderHash: hash of last rendered bytes (for change detection)
//   - LastRunID + LastRunAt: provenance for audit
//
// Why a cache (not just metadata embedded in the wiki):
//   - The wiki may have been edited by humans; their revisions aren't
//     ours and we still need to know what OUR last write was.
//   - Looking it up on every run by walking history is expensive on
//     long-lived pages.
//
// Backend: bbolt — pure-Go embedded B+tree, ACID, one file per wiki.
// Single-writer at runtime (we acquire an exclusive lock).
package runstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// bucketName is where SpecState values live inside the bolt file.
const bucketName = "spec-state"

// SpecState is the per-spec record persisted between runs.
type SpecState struct {
	LastBotRevID   string    `json:"last_bot_rev_id"`
	LastKBCommit   string    `json:"last_kb_commit"`
	LastRenderHash string    `json:"last_render_hash"`
	LastRunID      string    `json:"last_run_id"`
	LastRunAt      time.Time `json:"last_run_at"`
}

// Cache is the run-state store backed by bbolt.
type Cache struct {
	db *bolt.DB
}

// Open opens (or creates) the bolt file at path.
func Open(path string) (*Cache, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("runstate: open %s: %w", path, err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		return err
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("runstate: init bucket: %w", err)
	}
	return &Cache{db: db}, nil
}

// Close flushes pending writes and releases the file lock.
func (c *Cache) Close() error {
	if c.db == nil {
		return nil
	}
	return c.db.Close()
}

// Get returns the stored state for the given spec ID. ok=false
// indicates no record yet (first-time render).
func (c *Cache) Get(specID string) (SpecState, bool, error) {
	var s SpecState
	var ok bool
	err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return errors.New("runstate: bucket missing (open did not initialise)")
		}
		v := b.Get([]byte(specID))
		if v == nil {
			return nil
		}
		ok = true
		return json.Unmarshal(v, &s)
	})
	return s, ok, err
}

// Set overwrites the state for the given spec ID.
func (c *Cache) Set(specID string, s SpecState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("runstate: marshal: %w", err)
	}
	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return errors.New("runstate: bucket missing")
		}
		return b.Put([]byte(specID), data)
	})
}
