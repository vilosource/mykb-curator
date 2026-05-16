// Package sinks holds concrete reporter.Sink implementations
// (DESIGN.md §17 v1.0 "optional run-report sinks"): Slack webhook,
// email, and a kb workspace journal entry.
//
// Each sink takes its external boundary as an injected interface
// (HTTP doer / mail sender / command runner) so the sinks are
// deterministic and unit-testable with no network — the same
// boundary discipline used for the LLM and wiki adapters.
//
// All publishing is best-effort and observational; the MultiSink in
// the reporter package aggregates errors so one failing sink never
// blocks the others or the run.
package sinks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/vilosource/mykb-curator/internal/reporter"
)

// reportText is the shared human-readable body every sink sends: the
// one-line summary plus per-spec dispositions.
func reportText(r reporter.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "run=%s %s", r.RunID, r.Summary())
	for _, s := range r.Specs {
		fmt.Fprintf(&b, "\n  spec=%s status=%s blocks=%d %s", s.ID, s.Status, s.BlocksRegenerated, s.Reason)
	}
	return b.String()
}

// --- Slack ---

// HTTPDoer is the minimal HTTP boundary (net/http.Client satisfies
// it).
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Slack posts the run summary to an incoming-webhook URL.
type Slack struct {
	webhookURL string
	doer       HTTPDoer
}

// NewSlack constructs a Slack sink.
func NewSlack(webhookURL string, doer HTTPDoer) *Slack {
	return &Slack{webhookURL: webhookURL, doer: doer}
}

// Name returns "slack".
func (*Slack) Name() string { return "slack" }

// Publish POSTs a {"text": …} payload to the webhook.
func (s *Slack) Publish(ctx context.Context, r reporter.Report) error {
	payload, err := json.Marshal(map[string]string{"text": "mykb-curator run\n" + reportText(r)})
	if err != nil {
		return fmt.Errorf("slack: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("slack: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.doer.Do(req)
	if err != nil {
		return fmt.Errorf("slack: post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack: webhook returned %d: %s", resp.StatusCode, body)
	}
	return nil
}

// --- Email ---

// Sender is the mail boundary. The composition root wires an SMTP
// implementation; tests use a fake.
type Sender interface {
	Send(from string, to []string, subject string, body []byte) error
}

// Email sends the run summary as a plain-text message.
type Email struct {
	sender Sender
	from   string
	to     []string
}

// NewEmail constructs an Email sink.
func NewEmail(sender Sender, from string, to []string) *Email {
	return &Email{sender: sender, from: from, to: to}
}

// Name returns "email".
func (*Email) Name() string { return "email" }

// Publish sends the report body to the configured recipients.
func (e *Email) Publish(_ context.Context, r reporter.Report) error {
	subject := fmt.Sprintf("[mykb-curator] %s run %s", r.Wiki, r.RunID)
	if err := e.sender.Send(e.from, e.to, subject, []byte(reportText(r))); err != nil {
		return fmt.Errorf("email: send: %w", err)
	}
	return nil
}

// --- KB journal ---

// Runner is the command boundary used by the kb-journal sink. The
// real implementation shells out; tests use a fake.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// KBJournal appends the run summary to the active kb workspace's
// journal via `kb work journal "<summary>"`.
//
// NB: this writes back into a kb workspace. It assumes a workspace
// is active (the operator's environment); the mykb v2
// privileged-write-channel is the future direct path (v2 backlog).
type KBJournal struct {
	runner Runner
}

// NewKBJournal constructs the sink.
func NewKBJournal(runner Runner) *KBJournal { return &KBJournal{runner: runner} }

// Name returns "kb-journal".
func (*KBJournal) Name() string { return "kb-journal" }

// Publish runs `kb work journal "<summary>"`.
func (k *KBJournal) Publish(ctx context.Context, r reporter.Report) error {
	text := "mykb-curator run " + r.RunID + ": " + r.Summary()
	if err := k.runner.Run(ctx, "kb", "work", "journal", text); err != nil {
		return fmt.Errorf("kb-journal: %w", err)
	}
	return nil
}
