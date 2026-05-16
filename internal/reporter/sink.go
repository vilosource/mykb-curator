package reporter

import (
	"context"
	"errors"
	"fmt"
)

// Sink publishes a finalised run report to an external destination
// (Slack, email, a kb workspace journal, …). Implementations live in
// the reporter/sinks subpackage; each is constructed by the
// composition root from config.
//
// Publishing is observational, never load-bearing: a sink failure
// must not fail the curator run (the pages already shipped). The
// composition root logs sink errors; it does not abort on them.
type Sink interface {
	Name() string
	Publish(ctx context.Context, r Report) error
}

// MultiSink fans a report out to several sinks best-effort: every
// sink is attempted even if an earlier one fails, and all errors are
// aggregated so the caller can log them without losing any.
type MultiSink struct {
	sinks []Sink
}

// NewMultiSink composes sinks. Zero sinks = a valid no-op.
func NewMultiSink(sinks ...Sink) *MultiSink { return &MultiSink{sinks: sinks} }

// Name returns "multi".
func (*MultiSink) Name() string { return "multi" }

// Publish sends r to every sink, continuing past failures and
// returning the joined error (nil if all succeeded).
func (m *MultiSink) Publish(ctx context.Context, r Report) error {
	var errs []error
	for _, s := range m.sinks {
		if err := s.Publish(ctx, r); err != nil {
			errs = append(errs, fmt.Errorf("sink %q: %w", s.Name(), err))
		}
	}
	return errors.Join(errs...)
}
