// AnthropicClient is a minimal Anthropic Messages-API client.
//
// What it covers:
//   - POST /v1/messages with model + max_tokens + system + single
//     user message body
//   - x-api-key header auth
//   - parses the content[].text response into Response.Text
//   - propagates usage tokens
//
// What it deliberately doesn't:
//   - streaming (Server-Sent Events)
//   - tool use
//   - batch API
//   - retries with backoff (CacheDecorator handles re-runs)
//
// For Server-Sent Events / tool calls / etc. we'd swap to the
// official SDK, but the v0.5 frontends don't need any of that.
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

// AnthropicConfig is the constructor input for an AnthropicClient.
type AnthropicConfig struct {
	// Endpoint is the API base (e.g., https://api.anthropic.com).
	// Defaults to the production endpoint when empty.
	Endpoint string

	// APIKey is the Anthropic API key. Required.
	APIKey string

	// AnthropicVersion is the API version header. Defaults to
	// "2023-06-01".
	AnthropicVersion string

	// HTTPClient is optional; defaults to a sensible default with
	// 60s timeout when nil.
	HTTPClient *http.Client
}

// AnthropicClient implements Client by calling Anthropic's HTTPS API.
type AnthropicClient struct {
	cfg  AnthropicConfig
	http *http.Client
}

// NewAnthropicClient constructs a client from config.
func NewAnthropicClient(cfg AnthropicConfig) *AnthropicClient {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://api.anthropic.com"
	}
	if cfg.AnthropicVersion == "" {
		cfg.AnthropicVersion = "2023-06-01"
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &AnthropicClient{cfg: cfg, http: hc}
}

// messagesRequest mirrors a subset of Anthropic's POST /v1/messages
// schema. Only the fields the curator uses are modelled.
type messagesRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Messages  []messagesEntry `json:"messages"`
	StopSeq   []string        `json:"stop_sequences,omitempty"`
}

type messagesEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Complete invokes the Messages API.
func (c *AnthropicClient) Complete(ctx context.Context, req Request) (Response, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	body, err := json.Marshal(messagesRequest{
		Model:     req.Model,
		MaxTokens: maxTokens,
		System:    req.System,
		Messages:  []messagesEntry{{Role: "user", Content: req.Prompt}},
		StopSeq:   req.Stop,
	})
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	url := c.cfg.Endpoint + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", c.cfg.AnthropicVersion)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: http: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: read body: %w", err)
	}

	var msg messagesResponse
	if err := json.Unmarshal(respBody, &msg); err != nil {
		return Response{}, fmt.Errorf("anthropic: decode response (status=%d): %w; body=%s", httpResp.StatusCode, err, string(respBody))
	}
	if msg.Error != nil {
		return Response{}, fmt.Errorf("anthropic: api error %s: %s", msg.Error.Type, msg.Error.Message)
	}
	if httpResp.StatusCode >= 400 {
		return Response{}, fmt.Errorf("anthropic: http status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var text string
	for _, block := range msg.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return Response{
		Text:      text,
		TokensIn:  msg.Usage.InputTokens,
		TokensOut: msg.Usage.OutputTokens,
	}, nil
}
