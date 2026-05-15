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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
	text, err := s.invokePi(r, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	_ = json.NewEncoder(w).Encode(completeResponse{Text: text})
}

// invokePi is the bridge between this wrapper's HTTP contract and
// the pi CLI. The implementation is a stub for v0.0; the real
// invocation lands when we know pi's batch-mode flags.
func (s *server) invokePi(_ *http.Request, _ completeRequest) (string, error) {
	return "", errors.New("pi-wrapper v0.0: pi batch-mode invocation not wired yet (see DESIGN.md §17 v0.5 roadmap)")
}
