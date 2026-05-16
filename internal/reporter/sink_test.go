package reporter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vilosource/mykb-curator/internal/reporter"
)

type recordingSink struct {
	name string
	err  error
	got  *reporter.Report
}

func (s *recordingSink) Name() string { return s.name }
func (s *recordingSink) Publish(_ context.Context, r reporter.Report) error {
	rr := r
	s.got = &rr
	return s.err
}

func TestMultiSink_FanOutToAll(t *testing.T) {
	a := &recordingSink{name: "a"}
	b := &recordingSink{name: "b"}
	m := reporter.NewMultiSink(a, b)

	rep := reporter.NewBuilder("acme", "run-1").Build()
	if err := m.Publish(context.Background(), rep); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if a.got == nil || b.got == nil {
		t.Fatalf("not all sinks received the report: a=%v b=%v", a.got, b.got)
	}
	if a.got.RunID != "run-1" {
		t.Errorf("sink got RunID %q, want run-1", a.got.RunID)
	}
}

func TestMultiSink_BestEffort_ContinuesAndAggregates(t *testing.T) {
	boom := errors.New("slack 500")
	a := &recordingSink{name: "a", err: boom}
	b := &recordingSink{name: "b"} // must still receive despite a failing

	m := reporter.NewMultiSink(a, b)
	err := m.Publish(context.Background(), reporter.NewBuilder("w", "r").Build())
	if err == nil {
		t.Fatalf("expected aggregated error when a sink fails")
	}
	if !errors.Is(err, boom) {
		t.Errorf("aggregated error should wrap the failing sink's error, got %v", err)
	}
	if b.got == nil {
		t.Errorf("a failing sink must not prevent later sinks (best-effort)")
	}
}

func TestMultiSink_Empty_NoError(t *testing.T) {
	if err := reporter.NewMultiSink().Publish(context.Background(), reporter.Report{}); err != nil {
		t.Errorf("empty MultiSink should be a no-op, got %v", err)
	}
}
