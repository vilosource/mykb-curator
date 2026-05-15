package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ReplayClient returns fixture responses from a directory, keyed by
// sha256 of the request. Used in tests and CI to make LLM-dependent
// code deterministic.
//
// Missing fixtures are an error. Use RecordingClient (TBD) to generate
// fixtures against a real provider.
type ReplayClient struct {
	FixturesDir string
}

// NewReplayClient constructs a ReplayClient with the given fixtures
// directory.
func NewReplayClient(dir string) *ReplayClient {
	return &ReplayClient{FixturesDir: dir}
}

// Complete looks up a fixture for the request. The lookup key is the
// sha256 of "<model>|<prompt>|<system>|<max_tokens>".
func (c *ReplayClient) Complete(ctx context.Context, req Request) (Response, error) {
	key := requestHash(req)
	path := filepath.Join(c.FixturesDir, key+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Response{}, fmt.Errorf("llm replay: fixture not found for key %s (request: model=%q tokens=%d): %w", key, req.Model, req.MaxTokens, err)
	}
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return Response{}, fmt.Errorf("llm replay: decode fixture %s: %w", path, err)
	}
	resp.CacheHit = true
	return resp, nil
}

func requestHash(req Request) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%d", req.Model, req.Prompt, req.System, req.MaxTokens)
	for _, s := range req.Stop {
		fmt.Fprintf(h, "|stop:%s", s)
	}
	return hex.EncodeToString(h.Sum(nil))
}
