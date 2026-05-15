// RecordingClient is a write-through wrapper that always calls the
// inner Client and then persists the response to a fixtures dir
// using the same hash scheme as ReplayClient + CacheDecorator.
//
// Use cases:
//   - Regenerate the committed LLM fixture set after a prompt change.
//     Wrap a real provider (AnthropicClient), run the test suite,
//     review the diff, commit the new fixtures.
//   - Capture initial fixtures from a one-shot manual session.
//
// Distinction from CacheDecorator: Cache short-circuits on hit (no
// inner call); Recording ALWAYS calls inner (overwriting any
// existing fixture). Distinction from ReplayClient: Replay reads
// only; Recording writes.
//
// Failures from the inner client are propagated unmodified and NO
// fixture is written — transient errors don't poison the recording.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RecordingClient writes recorded responses to a fixtures dir.
type RecordingClient struct {
	inner Client
	dir   string
}

// NewRecordingClient wraps inner with a recorder rooted at dir.
// The directory is created on first write.
func NewRecordingClient(inner Client, dir string) *RecordingClient {
	return &RecordingClient{inner: inner, dir: dir}
}

// Complete calls the inner client; on success, persists the response
// to <dir>/<request-hash>.json (overwriting any existing fixture).
func (r *RecordingClient) Complete(ctx context.Context, req Request) (Response, error) {
	resp, err := r.inner.Complete(ctx, req)
	if err != nil {
		return Response{}, err
	}
	if writeErr := r.persist(req, resp); writeErr != nil {
		fmt.Fprintf(os.Stderr, "llm recording: persist failed (%v); response returned but not captured\n", writeErr)
	}
	return resp, nil
}

func (r *RecordingClient) persist(req Request, resp Response) error {
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return err
	}
	// Don't persist transient flags; the next ReplayClient read sets
	// CacheHit on the way out.
	resp.CacheHit = false
	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(r.dir, requestHash(req)+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
