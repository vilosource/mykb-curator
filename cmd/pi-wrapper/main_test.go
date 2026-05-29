package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fixturePath resolves test/fixtures/pi/pong.jsonl relative to this
// source file so the test is CWD-independent.
func fixturePath(t *testing.T) string {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(self), "..", "..", "test", "fixtures", "pi", "pong.jsonl")
}

func TestParsePiStream_RealFixture(t *testing.T) {
	f, err := os.Open(fixturePath(t))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	got, err := parsePiStream(f)
	if err != nil {
		t.Fatalf("parsePiStream: %v", err)
	}
	if got.Text != "pong" {
		t.Errorf("Text = %q, want %q", got.Text, "pong")
	}
	// agent_end's final assistant usage in the fixture: input=615
	// output=6 (verified during the spike).
	if got.TokensIn != 615 || got.TokensOut != 6 {
		t.Errorf("tokens = %d/%d, want 615/6", got.TokensIn, got.TokensOut)
	}
}

func TestParsePiStream_TurnEndFallback(t *testing.T) {
	// No agent_end; must fall back to the last turn_end assistant.
	stream := `{"type":"session"}
{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}],"usage":{"input":3,"output":2}}}`
	got, err := parsePiStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parsePiStream: %v", err)
	}
	if got.Text != "hello world" || got.TokensIn != 3 || got.TokensOut != 2 {
		t.Errorf("got %+v", got)
	}
}

func TestParsePiStream_MessageEndFallback(t *testing.T) {
	stream := `{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":"q"}]}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"A"}],"usage":{"input":1,"output":1}}}`
	got, err := parsePiStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parsePiStream: %v", err)
	}
	if got.Text != "A" {
		t.Errorf("Text = %q", got.Text)
	}
}

func TestParsePiStream_NoiseLinesIgnored(t *testing.T) {
	stream := "not json at all\n" +
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input":5,"output":1}}]}` + "\n" +
		"trailing garbage"
	got, err := parsePiStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parsePiStream: %v", err)
	}
	if got.Text != "ok" {
		t.Errorf("Text = %q", got.Text)
	}
}

func TestParsePiStream_NoAssistantIsError(t *testing.T) {
	if _, err := parsePiStream(strings.NewReader(`{"type":"session"}`)); err == nil {
		t.Errorf("stream with no assistant message must error, not return empty")
	}
}

func TestParsePiStream_EmptyTextIsError(t *testing.T) {
	stream := `{"type":"agent_end","messages":[{"role":"assistant","content":[],"usage":{"input":1,"output":0}}]}`
	if _, err := parsePiStream(strings.NewReader(stream)); err == nil {
		t.Errorf("assistant message with no text content must error")
	}
}

// fakePi writes a shell script acting as the `pi` binary: it echoes
// the canned stream to stdout (or exits non-zero if FAIL=1).
func fakePi(t *testing.T, stream string, fail bool) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "pi")
	body := "#!/bin/sh\n"
	if fail {
		body += "echo 'pi: provider auth failed' >&2\nexit 1\n"
	} else {
		// Single-quote the heredoc so the JSON is emitted verbatim.
		body += "cat <<'PIEOF'\n" + stream + "\nPIEOF\n"
	}
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake pi: %v", err)
	}
	return bin
}

// fakePiCapture writes a fake `pi` that records its argv (one line per
// arg) to <dir>/argv and its stdin to <dir>/stdin, then emits the
// canned stream. Returns (binPath, dir) so the test can inspect what
// pi actually received.
func fakePiCapture(t *testing.T, stream string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "pi")
	body := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\"; done > " + filepath.Join(dir, "argv") + "\n" +
		"cat > " + filepath.Join(dir, "stdin") + "\n" +
		"cat <<'PIEOF'\n" + stream + "\nPIEOF\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake pi: %v", err)
	}
	return bin, dir
}

// The prompt must travel on stdin, never as a trailing argv arg:
// grounded curator prompts routinely exceed the OS arg limit and a
// positional arg fails with "argument list too long". pi reads piped
// stdin and prepends it to the initial message (verified against
// pi-coding-agent@0.66.1).
func TestInvokePi_PromptDeliveredViaStdinNotArgv(t *testing.T) {
	stream := `{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input":1,"output":1}}]}`
	bin, dir := fakePiCapture(t, stream)
	s := &server{piBin: bin}

	const marker = "GROUNDING-MARKER"
	prompt := marker + " " + strings.Repeat("x", 4096)
	if _, err := s.invokePi(context.Background(), completeRequest{
		Prompt: prompt, Model: "google/gemini-2.5-flash", System: "be terse",
	}); err != nil {
		t.Fatalf("invokePi: %v", err)
	}

	argv, err := os.ReadFile(filepath.Join(dir, "argv"))
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	stdin, err := os.ReadFile(filepath.Join(dir, "stdin"))
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	if strings.Contains(string(argv), marker) {
		t.Errorf("prompt must NOT be an argv arg (ARG_MAX overflow); argv:\n%s", argv)
	}
	if !strings.Contains(string(stdin), marker) {
		t.Errorf("prompt must be delivered on stdin; got stdin=%q", stdin)
	}
	// Flags + system prompt still go via argv (they are small + bounded).
	if !strings.Contains(string(argv), "--system-prompt") || !strings.Contains(string(argv), "--mode") {
		t.Errorf("expected flags + --system-prompt on argv, got:\n%s", argv)
	}
}

// A single argv arg above the Linux per-arg limit (MAX_ARG_STRLEN,
// 128KB) fails with E2BIG ("argument list too long") — the exact bug.
// Delivering the prompt on stdin removes the limit.
func TestInvokePi_LargePromptExceedingArgMax(t *testing.T) {
	stream := `{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input":1,"output":1}}]}`
	bin, _ := fakePiCapture(t, stream)
	s := &server{piBin: bin}

	prompt := strings.Repeat("g", 300*1024) // 300KB, well over MAX_ARG_STRLEN
	resp, err := s.invokePi(context.Background(), completeRequest{Prompt: prompt})
	if err != nil {
		t.Fatalf("large prompt must succeed via stdin, got: %v", err)
	}
	if resp.Text != "ok" {
		t.Errorf("resp.Text = %q, want ok", resp.Text)
	}
}

func TestInvokePi_HappyPath(t *testing.T) {
	stream := `{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"from-fake-pi"}],"usage":{"input":9,"output":2}}]}`
	s := &server{piBin: fakePi(t, stream, false)}

	resp, err := s.invokePi(context.Background(), completeRequest{Prompt: "hi", Model: "anthropic/claude", System: "be terse"})
	if err != nil {
		t.Fatalf("invokePi: %v", err)
	}
	if resp.Text != "from-fake-pi" || resp.TokensIn != 9 || resp.TokensOut != 2 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestInvokePi_PiFailureSurfaces(t *testing.T) {
	s := &server{piBin: fakePi(t, "", true)}
	_, err := s.invokePi(context.Background(), completeRequest{Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "pi exec failed") {
		t.Errorf("expected pi exec failure, got %v", err)
	}
}

func TestComplete_HTTPEndToEnd(t *testing.T) {
	stream := `{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"http-ok"}],"usage":{"input":4,"output":1}}]}`
	s := &server{piBin: fakePi(t, stream, false)}

	body, _ := json.Marshal(completeRequest{Prompt: "render this"})
	req := httptest.NewRequest(http.MethodPost, "/complete", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.complete(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got completeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Text != "http-ok" || got.TokensIn != 4 {
		t.Errorf("got %+v", got)
	}
}

func TestComplete_EmptyPromptRejected(t *testing.T) {
	s := &server{piBin: "pi"}
	req := httptest.NewRequest(http.MethodPost, "/complete", strings.NewReader(`{"prompt":"  "}`))
	rec := httptest.NewRecorder()
	s.complete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty prompt status = %d, want 400", rec.Code)
	}
}
