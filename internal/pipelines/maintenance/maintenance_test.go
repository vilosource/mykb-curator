package maintenance

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
)

// fakeCheck returns canned proposals (or an error). Records the
// snapshot it received so tests can assert wiring.
type fakeCheck struct {
	name    string
	gotSnap kb.Snapshot
	out     []MutationProposal
	err     error
}

func (f *fakeCheck) Name() string { return f.name }
func (f *fakeCheck) Run(_ context.Context, snap kb.Snapshot) ([]MutationProposal, error) {
	f.gotSnap = snap
	return f.out, f.err
}

func TestPipeline_Empty_ReturnsNoProposals(t *testing.T) {
	p := NewPipeline()
	got, err := p.Run(context.Background(), kb.Snapshot{})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d proposals, want 0", len(got))
	}
}

func TestPipeline_AggregatesProposalsFromAllChecks(t *testing.T) {
	c1 := &fakeCheck{name: "c1", out: []MutationProposal{
		{Kind: ProposalVerify, Area: "vault", ID: "f1", Source: "c1"},
	}}
	c2 := &fakeCheck{name: "c2", out: []MutationProposal{
		{Kind: ProposalDeprecate, Area: "harbor", ID: "g1", Source: "c2"},
		{Kind: ProposalAdd, Area: "vault", ID: "f-new", Source: "c2", Text: "new fact"},
	}}
	got, err := NewPipeline(c1, c2).Run(context.Background(), kb.Snapshot{Commit: "abc"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3 (1 from c1 + 2 from c2)", len(got))
	}
}

func TestPipeline_StopsOnFirstError(t *testing.T) {
	wantErr := errors.New("simulated")
	c1 := &fakeCheck{name: "ok", out: []MutationProposal{{Kind: ProposalVerify, Area: "x", ID: "y"}}}
	c2 := &fakeCheck{name: "boom", err: wantErr}
	c3 := &fakeCheck{name: "should-not-run"}

	_, err := NewPipeline(c1, c2, c3).Run(context.Background(), kb.Snapshot{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wraps %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err should name failing check; got %v", err)
	}
	if c3.gotSnap.Commit != "" {
		// c3 ran despite earlier error — wiring bug.
		t.Errorf("c3 ran after c2 errored")
	}
}

func TestPipeline_ThreadsSameSnapToEveryCheck(t *testing.T) {
	c1 := &fakeCheck{name: "c1"}
	c2 := &fakeCheck{name: "c2"}
	snap := kb.Snapshot{Commit: "shared-commit"}
	_, _ = NewPipeline(c1, c2).Run(context.Background(), snap)
	if c1.gotSnap.Commit != "shared-commit" || c2.gotSnap.Commit != "shared-commit" {
		t.Errorf("c1=%q c2=%q, want both shared-commit", c1.gotSnap.Commit, c2.gotSnap.Commit)
	}
}

func TestMutationProposal_Kinds(t *testing.T) {
	// Sanity: the three kinds are distinct values.
	kinds := []ProposalKind{ProposalVerify, ProposalDeprecate, ProposalAdd}
	seen := map[ProposalKind]bool{}
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("duplicate kind value: %v", k)
		}
		seen[k] = true
	}
}
