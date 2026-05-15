// CacheDecorator wraps an LLM Client with a filesystem cache.
//
// Cache layout: one <hash>.json file per request in a flat
// directory. Same scheme ReplayClient uses, so files can be promoted
// from runtime cache → committed test fixture by `cp` alone.
//
// Errors are NOT cached — the cache only memoises successful
// responses. A transient network failure on the inner client doesn't
// poison the cache for the same prompt.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// CacheDecorator is an LLM Client middleware that memoises responses
// to disk keyed by sha256(model|prompt|system|max_tokens|stops).
type CacheDecorator struct {
	inner Client
	dir   string
}

// NewCacheDecorator wraps inner with a cache rooted at dir.
// The directory is created on first write.
func NewCacheDecorator(inner Client, dir string) *CacheDecorator {
	return &CacheDecorator{inner: inner, dir: dir}
}

// Complete returns a cached response if present; otherwise calls the
// inner client and persists the result.
func (c *CacheDecorator) Complete(ctx context.Context, req Request) (Response, error) {
	key := requestHash(req)
	path := filepath.Join(c.dir, key+".json")

	if cached, ok := c.tryRead(path); ok {
		cached.CacheHit = true
		return cached, nil
	}

	resp, err := c.inner.Complete(ctx, req)
	if err != nil {
		return Response{}, err
	}
	if writeErr := c.persist(path, resp); writeErr != nil {
		// Persist failure is non-fatal — return the response anyway.
		// The next run will re-call the inner; correctness preserved.
		// Stderr is the right channel for cache hiccups; tests can
		// suppress with their own logger if needed.
		fmt.Fprintf(os.Stderr, "llm cache: persist failed (%v); proceeding without caching this entry\n", writeErr)
	}
	return resp, nil
}

func (c *CacheDecorator) tryRead(path string) (Response, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Response{}, false
		}
		return Response{}, false
	}
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return Response{}, false
	}
	return resp, true
}

func (c *CacheDecorator) persist(path string, resp Response) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	// Don't store CacheHit on disk — that field is derived at read.
	resp.CacheHit = false
	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
