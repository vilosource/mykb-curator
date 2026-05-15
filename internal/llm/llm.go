// Package llm defines the Client interface for LLM completions and
// houses the concrete clients (anthropic, pi, replay, recording).
//
// The curator depends on the interface; the composition root wires
// the concrete client per config.
package llm

import "context"

// Client is the LLM boundary. All LLM-using components (editorial
// frontend, expensive checks, polish-prose pass) depend on this
// interface, not on any provider SDK.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// Request is one completion call.
type Request struct {
	Model     string
	Prompt    string
	System    string // optional system prompt
	MaxTokens int    // 0 = provider default
	Stop      []string
}

// Response is the result of one completion call.
type Response struct {
	Text      string
	TokensIn  int
	TokensOut int
	// CacheHit indicates the response came from a cache layer
	// (Replay, decorated cache). Useful for observability.
	CacheHit bool
}
