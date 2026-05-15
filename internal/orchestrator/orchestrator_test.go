package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/reporter"
)

// fakeKB returns a fixed snapshot.
type fakeKB struct {
	commit string
	err    error
}

func (f fakeKB) Pull(ctx context.Context) (kb.Snapshot, error) {
	if f.err != nil {
		return kb.Snapshot{}, f.err
	}
	return kb.Snapshot{Commit: f.commit}, nil
}
func (fakeKB) Whoami() string { return "fakeKB" }

// fakeSpecs returns a fixed slice.
type fakeSpecs struct {
	items []specs.Spec
	err   error
}

func (f fakeSpecs) Pull(ctx context.Context) ([]specs.Spec, error) {
	return f.items, f.err
}
func (fakeSpecs) Whoami() string { return "fakeSpecs" }

// fakeWiki is unused in the v0.0 skeleton — no spec actually renders.
// Provided so the orchestrator can be constructed.
type fakeWiki struct{}

func (fakeWiki) Whoami(ctx context.Context) (string, error)                { return "User:Fake", nil }
func (fakeWiki) GetPage(ctx context.Context, _ string) (*wiki.Page, error) { return nil, nil }
func (fakeWiki) UpsertPage(ctx context.Context, _, _, _ string) (wiki.Revision, error) {
	return wiki.Revision{}, nil
}
func (fakeWiki) History(ctx context.Context, _, _ string) ([]wiki.Revision, error) { return nil, nil }
func (fakeWiki) HumanEditsSinceBot(ctx context.Context, _, _ string) (*wiki.HumanEdit, error) {
	return nil, nil
}

// fakeLLM is unused for v0.0 but satisfies the interface.
type fakeLLM struct{}

func (fakeLLM) Complete(ctx context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{}, nil
}

func TestRun_HappyPath_RecordsAllSpecsAsSkipped(t *testing.T) {
	o := New(Deps{
		Wiki: "acme",
		KB:   fakeKB{commit: "abc123"},
		Specs: fakeSpecs{items: []specs.Spec{
			{ID: "page-a", Wiki: "acme", Page: "PageA", Kind: "projection"},
			{ID: "page-b", Wiki: "acme", Page: "PageB", Kind: "editorial"},
		}},
		WikiTarget: fakeWiki{},
		LLM:        fakeLLM{},
	})

	rep, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.KBCommit != "abc123" {
		t.Errorf("KBCommit = %q, want %q", rep.KBCommit, "abc123")
	}
	if len(rep.Specs) != 2 {
		t.Fatalf("len(Specs) = %d, want 2", len(rep.Specs))
	}
	for _, s := range rep.Specs {
		if s.Status != reporter.StatusSkipped {
			t.Errorf("spec %s: status = %q, want %q", s.ID, s.Status, reporter.StatusSkipped)
		}
	}
}

func TestRun_WikiMismatch_FailsSpec(t *testing.T) {
	o := New(Deps{
		Wiki: "acme",
		KB:   fakeKB{commit: "abc"},
		Specs: fakeSpecs{items: []specs.Spec{
			{ID: "bad", Wiki: "widgetco", Page: "X"},
		}},
		WikiTarget: fakeWiki{},
		LLM:        fakeLLM{},
	})

	rep, _ := o.Run(context.Background())
	if len(rep.Specs) != 1 {
		t.Fatalf("len(Specs) = %d, want 1", len(rep.Specs))
	}
	if rep.Specs[0].Status != reporter.StatusFailed {
		t.Errorf("status = %q, want %q", rep.Specs[0].Status, reporter.StatusFailed)
	}
}

func TestRun_KBPullError_PropagatesAndReports(t *testing.T) {
	wantErr := errors.New("simulated kb failure")
	o := New(Deps{
		Wiki:       "acme",
		KB:         fakeKB{err: wantErr},
		Specs:      fakeSpecs{},
		WikiTarget: fakeWiki{},
		LLM:        fakeLLM{},
	})
	rep, err := o.Run(context.Background())
	if err == nil {
		t.Errorf("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wraps %v", err, wantErr)
	}
	if len(rep.Errors) != 1 {
		t.Errorf("Errors = %v, want 1", rep.Errors)
	}
}
