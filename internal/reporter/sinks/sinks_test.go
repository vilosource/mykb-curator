package sinks_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/reporter"
	"github.com/vilosource/mykb-curator/internal/reporter/sinks"
)

func sampleReport() reporter.Report {
	b := reporter.NewBuilder("acme", "run-42")
	b.SetKBCommit("abc123")
	b.AddSpecResult(reporter.SpecResult{ID: "p1", Status: reporter.StatusRendered})
	return b.Build()
}

// --- Slack ---

type fakeDoer struct {
	req  *http.Request
	body string
	resp *http.Response
	err  error
}

func (f *fakeDoer) Do(r *http.Request) (*http.Response, error) {
	f.req = r
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		f.body = string(b)
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
}

func TestSlack_PostsSummaryToWebhook(t *testing.T) {
	d := &fakeDoer{}
	s := sinks.NewSlack("https://hooks.slack.test/xyz", d)
	if s.Name() != "slack" {
		t.Errorf("Name = %q", s.Name())
	}
	if err := s.Publish(context.Background(), sampleReport()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if d.req == nil || d.req.Method != http.MethodPost {
		t.Fatalf("expected POST, got %+v", d.req)
	}
	if d.req.URL.String() != "https://hooks.slack.test/xyz" {
		t.Errorf("URL = %s", d.req.URL)
	}
	if !strings.Contains(d.body, "run-42") || !strings.Contains(d.body, `"text"`) {
		t.Errorf("payload missing summary/text: %s", d.body)
	}
}

func TestSlack_Non2xxIsError(t *testing.T) {
	d := &fakeDoer{resp: &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("boom"))}}
	s := sinks.NewSlack("https://x", d)
	if err := s.Publish(context.Background(), sampleReport()); err == nil {
		t.Errorf("expected error on non-2xx Slack response")
	}
}

// --- Email ---

type fakeSender struct {
	from    string
	to      []string
	subject string
	body    string
	err     error
}

func (f *fakeSender) Send(from string, to []string, subject string, body []byte) error {
	f.from, f.to, f.subject, f.body = from, to, subject, string(body)
	return f.err
}

func TestEmail_SendsSummary(t *testing.T) {
	fs := &fakeSender{}
	s := sinks.NewEmail(fs, "curator@acme.test", []string{"ops@acme.test"})
	if s.Name() != "email" {
		t.Errorf("Name = %q", s.Name())
	}
	if err := s.Publish(context.Background(), sampleReport()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if fs.from != "curator@acme.test" || len(fs.to) != 1 || fs.to[0] != "ops@acme.test" {
		t.Errorf("envelope wrong: from=%q to=%v", fs.from, fs.to)
	}
	if !strings.Contains(fs.subject, "acme") || !strings.Contains(fs.body, "run-42") {
		t.Errorf("subject/body missing run info: subj=%q body=%q", fs.subject, fs.body)
	}
}

func TestEmail_SenderErrorPropagates(t *testing.T) {
	s := sinks.NewEmail(&fakeSender{err: errors.New("smtp down")}, "f@x", []string{"t@x"})
	if err := s.Publish(context.Background(), sampleReport()); err == nil {
		t.Errorf("expected sender error to propagate")
	}
}

// --- KB journal ---

type fakeRunner struct {
	name string
	args []string
	err  error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	f.name, f.args = name, args
	return f.err
}

func TestKBJournal_RunsKbWorkJournal(t *testing.T) {
	fr := &fakeRunner{}
	s := sinks.NewKBJournal(fr)
	if s.Name() != "kb-journal" {
		t.Errorf("Name = %q", s.Name())
	}
	if err := s.Publish(context.Background(), sampleReport()); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if fr.name != "kb" {
		t.Fatalf("expected to invoke kb, got %q", fr.name)
	}
	// kb work journal "<summary text>"
	if len(fr.args) != 3 || fr.args[0] != "work" || fr.args[1] != "journal" {
		t.Fatalf("args = %v, want [work journal <text>]", fr.args)
	}
	if !strings.Contains(fr.args[2], "run-42") {
		t.Errorf("journal text missing run summary: %q", fr.args[2])
	}
}

func TestKBJournal_RunnerErrorPropagates(t *testing.T) {
	s := sinks.NewKBJournal(&fakeRunner{err: errors.New("kb missing")})
	if err := s.Publish(context.Background(), sampleReport()); err == nil {
		t.Errorf("expected runner error to propagate")
	}
}
