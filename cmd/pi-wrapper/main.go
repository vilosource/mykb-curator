// Command pi-wrapper is a thin HTTP shim around the `pi` CLI.
//
// It runs inside the Pi-harness test container and exposes:
//
//	POST /complete   { "prompt": "...", "model": "...", "max_tokens": N }
//	                 → { "text": "...", "tokens_in": N, "tokens_out": N }
//	GET  /healthz    → 200 if pi is reachable
//
// This exists because the curator's PiClient LLM impl talks HTTP to a
// container, and the `pi` CLI is interactive by default. If Pi gains
// a native HTTP server mode later, this wrapper can be retired.
//
// The wrapper is intentionally minimal — no auth, no rate limiting,
// no retries. It is for test use inside a docker network only.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// piBinary is the `pi` executable name. Override at build time or
// via PI_BINARY env if the harness installs it under a different name.
var piBinary = "pi"

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	piBin := flag.String("pi", piBinary, "pi binary (path or name on $PATH)")
	flag.Parse()

	srv := &server{piBin: *piBin}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", srv.healthz)
	mux.HandleFunc("/complete", srv.complete)

	log.Printf("pi-wrapper listening on %s (pi=%s)", *addr, *piBin)
	httpsrv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(httpsrv.ListenAndServe())
}

type server struct {
	piBin string
}

type completeRequest struct {
	Prompt    string `json:"prompt"`
	Model     string `json:"model,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	System    string `json:"system,omitempty"`
}

type completeResponse struct {
	Text      string `json:"text"`
	TokensIn  int    `json:"tokens_in,omitempty"`
	TokensOut int    `json:"tokens_out,omitempty"`
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	// Cheapest possible reachability check: pi --version.
	cmd := exec.CommandContext(r.Context(), s.piBin, "--version")
	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("pi binary not reachable: %v", err), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *server) complete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req completeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "prompt: required", http.StatusBadRequest)
		return
	}

	// NOTE: the exact pi batch-mode flags are TBD — the user has
	// committed to using pi but the harness contract (which flag turns
	// it into a single-shot batch consumer) is finalised in the
	// follow-up PR that wires the real PiClient. For v0.0, we shell out
	// to a placeholder invocation and surface a clear error if pi is
	// not configured for batch mode.
	resp, err := s.invokePi(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// invokePi runs the pi CLI in single-shot batch mode and parses its
// JSONL event stream. The flag set + stream schema are frozen in
// docs/pi-harness-contract.md (resolved by the design spike).
//
//	pi --print --mode json --no-tools --no-session --no-extensions
//	   --no-skills [--model M] [--system-prompt S] <prompt>
//
// --no-tools/-session/-extensions/-skills keep this a plain LLM call,
// not the operator's kb-pi agent. MaxTokens has no pi flag and is
// intentionally ignored (documented in the contract).
func (s *server) invokePi(ctx context.Context, req completeRequest) (completeResponse, error) {
	args := []string{
		"--print", "--mode", "json",
		"--no-tools", "--no-session", "--no-extensions", "--no-skills",
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if strings.TrimSpace(req.System) != "" {
		args = append(args, "--system-prompt", req.System)
	}
	args = append(args, req.Prompt)

	cmd := exec.CommandContext(ctx, s.piBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return completeResponse{}, fmt.Errorf("pi exec failed: %v; stderr=%s", err, strings.TrimSpace(stderr.String()))
	}
	resp, err := parsePiStream(&stdout)
	if err != nil {
		return completeResponse{}, err
	}
	return resp, nil
}

// piMsg / piEvent mirror the subset of the pi --mode json event
// schema we depend on (docs/pi-harness-contract.md).
type piMsg struct {
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		Input  int `json:"input"`
		Output int `json:"output"`
	} `json:"usage"`
}

type piEvent struct {
	Type     string  `json:"type"`
	Messages []piMsg `json:"messages"` // agent_end
	Message  *piMsg  `json:"message"`  // turn_end / message_end
}

// parsePiStream extracts the final assistant message from pi's JSONL
// event stream per the frozen contract: prefer the last agent_end
// (its last assistant message), then the last turn_end message, then
// the last assistant message_end. Empty / no-assistant ⇒ error
// (fail-loud; never a silent empty success).
func parsePiStream(r io.Reader) (completeResponse, error) {
	var final *piMsg
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev piEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // robust to interleaved non-JSON noise
		}
		switch ev.Type {
		case "agent_end":
			for i := len(ev.Messages) - 1; i >= 0; i-- {
				if ev.Messages[i].Role == "assistant" {
					m := ev.Messages[i]
					final = &m
					break
				}
			}
		case "turn_end":
			if ev.Message != nil && ev.Message.Role == "assistant" {
				final = ev.Message
			}
		case "message_end":
			if ev.Message != nil && ev.Message.Role == "assistant" {
				final = ev.Message
			}
		}
	}
	if err := sc.Err(); err != nil {
		return completeResponse{}, fmt.Errorf("pi stream read: %w", err)
	}
	if final == nil {
		return completeResponse{}, errors.New("pi produced no assistant message")
	}
	var text strings.Builder
	for _, c := range final.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	if text.Len() == 0 {
		return completeResponse{}, errors.New("pi assistant message had no text content")
	}
	return completeResponse{
		Text:      text.String(),
		TokensIn:  final.Usage.Input,
		TokensOut: final.Usage.Output,
	}, nil
}
