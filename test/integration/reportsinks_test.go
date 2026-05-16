//go:build integration

package integration_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/reporter"
	"github.com/vilosource/mykb-curator/internal/reporter/sinks"
)

type rsDoer struct {
	hit  bool
	fail bool
}

func (d *rsDoer) Do(*http.Request) (*http.Response, error) {
	d.hit = true
	code := 200
	if d.fail {
		code = 500
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(""))}, nil
}

type rsSender struct{ got bool }

func (s *rsSender) Send(_ string, _ []string, _ string, _ []byte) error {
	s.got = true
	return nil
}

type rsRunner struct {
	got  bool
	args []string
	err  error
}

func (r *rsRunner) Run(_ context.Context, _ string, args ...string) error {
	r.got, r.args = true, args
	return r.err
}

// TestReportSinks_MultiSinkFanOut composes the three real sink
// implementations behind a real reporter.MultiSink and asserts
// best-effort fan-out: a failing Slack sink does not stop the email
// or kb-journal sinks, and the failure is surfaced as an aggregated
// error (cross-package: reporter + sinks).
func TestReportSinks_MultiSinkFanOut(t *testing.T) {
	doer := &rsDoer{fail: true} // Slack will 500
	sender := &rsSender{}
	runner := &rsRunner{}

	multi := reporter.NewMultiSink(
		sinks.NewSlack("https://hook.test", doer),
		sinks.NewEmail(sender, "curator@test", []string{"ops@test"}),
		sinks.NewKBJournal(runner),
	)

	b := reporter.NewBuilder("acme", "run-integ")
	b.SetKBCommit("deadbeef")
	b.AddSpecResult(reporter.SpecResult{ID: "p1", Status: reporter.StatusRendered})
	rep := b.Build()

	err := multi.Publish(context.Background(), rep)
	if err == nil {
		t.Fatalf("expected aggregated error from the failing Slack sink")
	}
	if !strings.Contains(err.Error(), "slack") {
		t.Errorf("aggregated error should name the slack sink: %v", err)
	}
	if !doer.hit {
		t.Errorf("slack sink not invoked")
	}
	if !sender.got {
		t.Errorf("email sink must run despite slack failing (best-effort)")
	}
	if !runner.got {
		t.Errorf("kb-journal sink must run despite slack failing (best-effort)")
	}
	if len(runner.args) != 3 || runner.args[1] != "journal" || !strings.Contains(runner.args[2], "run-integ") {
		t.Errorf("kb-journal args wrong: %v", runner.args)
	}
}

func TestReportSinks_AllGreen(t *testing.T) {
	doer := &rsDoer{}
	sender := &rsSender{}
	runner := &rsRunner{}
	multi := reporter.NewMultiSink(
		sinks.NewSlack("https://hook.test", doer),
		sinks.NewEmail(sender, "f@test", []string{"t@test"}),
		sinks.NewKBJournal(runner),
	)
	if err := multi.Publish(context.Background(), reporter.NewBuilder("w", "r").Build()); err != nil {
		t.Fatalf("all-green publish should not error: %v", err)
	}
	if !doer.hit || !sender.got || !runner.got {
		t.Errorf("not all sinks ran: slack=%v email=%v kb=%v", doer.hit, sender.got, runner.got)
	}
}
