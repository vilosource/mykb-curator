// PiClient is the LLM impl that drives a live Pi agent via the
// pi-harness HTTP shim (DESIGN §15/§16; contract in
// docs/pi-harness-contract.md).
//
// Deliberately HTTP-only: PiClient never parses Pi's JSONL event
// stream itself. The shim (cmd/pi-wrapper) owns the exec + stream
// parsing; PiClient just speaks the shim's stable JSON contract
// (POST /complete {prompt,model,max_tokens,system} →
// {text,tokens_in,tokens_out}). That process boundary is the whole
// point of the harness design — keeps the curator free of pi-CLI
// coupling and keeps PiClient unit-testable with a plain httptest
// server.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PiConfig is the constructor input for a PiClient.
type PiConfig struct {
	// Endpoint is the pi-harness base URL (e.g. http://pi-harness:8080).
	// The "/complete" path is appended by the client.
	Endpoint string

	// HTTPClient is optional; defaults to a 120s-timeout client when
	// nil (a real agent turn is slower than a raw API call).
	HTTPClient *http.Client
}

// PiClient implements Client by calling the pi-harness shim.
type PiClient struct {
	cfg  PiConfig
	http *http.Client
}

// NewPiClient constructs a client for the given pi-harness endpoint.
func NewPiClient(endpoint string) *PiClient {
	return NewPiClientWithConfig(PiConfig{Endpoint: endpoint})
}

// NewPiClientWithConfig is the full-control constructor.
func NewPiClientWithConfig(cfg PiConfig) *PiClient {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 120 * time.Second}
	}
	return &PiClient{cfg: cfg, http: hc}
}

type piCompleteRequest struct {
	Prompt    string `json:"prompt"`
	Model     string `json:"model,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	System    string `json:"system,omitempty"`
}

type piCompleteResponse struct {
	Text      string `json:"text"`
	TokensIn  int    `json:"tokens_in"`
	TokensOut int    `json:"tokens_out"`
}

// Complete sends one completion to the pi-harness and maps the
// response back onto llm.Response.
func (c *PiClient) Complete(ctx context.Context, req Request) (Response, error) {
	payload, err := json.Marshal(piCompleteRequest{
		Prompt:    req.Prompt,
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    req.System,
	})
	if err != nil {
		return Response{}, fmt.Errorf("pi: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint+"/complete", bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("pi: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("pi: post %s/complete: %w", c.cfg.Endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("pi: harness returned %d: %s", resp.StatusCode, body)
	}

	var pr piCompleteResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return Response{}, fmt.Errorf("pi: decode response: %w; body=%s", err, body)
	}
	if pr.Text == "" {
		// The shim guarantees non-empty text or a non-2xx (see
		// docs/pi-harness-contract.md). A 2xx with empty text is a
		// contract violation — fail loud, never a silent "".
		return Response{}, fmt.Errorf("pi: harness returned empty completion text (contract violation)")
	}
	return Response{
		Text:      pr.Text,
		TokensIn:  pr.TokensIn,
		TokensOut: pr.TokensOut,
	}, nil
}
