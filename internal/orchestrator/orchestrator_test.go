package orchestrator

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/memory"
	"github.com/vilosource/mykb-curator/internal/cache/ircache"
	"github.com/vilosource/mykb-curator/internal/cache/runstate"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
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
func (fakeKB) DiffSince(context.Context, string) ([]string, error) {
	return nil, kb.ErrDiffNotSupported
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
func (fakeWiki) UploadFile(context.Context, string, []byte, string, string) (string, error) {
	return "File:fake.png", nil
}

// fakeLLM is unused for v0.0 but satisfies the interface.
type fakeLLM struct{}

func (fakeLLM) Complete(ctx context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{}, nil
}

func TestRun_HappyPath_NoPipeline_RecordsSkipped(t *testing.T) {
	// With Frontends == nil, the rendering pipeline is not wired and
	// every valid spec is recorded as Skipped. This preserves the
	// v0.0 walking-skeleton behaviour for partial deployments.
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

// fakeFrontend builds a tiny Document so the rendering pipeline has
// something to push through.
type fakeFrontend struct{}

func (fakeFrontend) Name() string { return "fake-frontend" }
func (fakeFrontend) Kind() string { return "projection" }
func (fakeFrontend) Build(_ context.Context, s specs.Spec, _ kb.Snapshot) (ir.Document, error) {
	return ir.Document{
		Frontmatter: ir.Frontmatter{Title: s.Page},
		Sections: []ir.Section{{
			Heading: "Generated",
			Blocks:  []ir.Block{ir.ProseBlock{Text: "from " + s.ID}},
		}},
	}, nil
}

// fakeBackend renders by concatenating prose-block text. Pure.
type fakeBackend struct{}

func (fakeBackend) Name() string { return "fake-backend" }
func (fakeBackend) Render(doc ir.Document) ([]byte, error) {
	var s string
	for _, sec := range doc.Sections {
		for _, b := range sec.Blocks {
			if p, ok := b.(ir.ProseBlock); ok {
				s += p.Text + "\n"
			}
		}
	}
	return []byte(s), nil
}

func TestRun_WiredPipeline_RendersSpec(t *testing.T) {
	reg := frontends.NewRegistry()
	reg.Register(fakeFrontend{})

	var captured map[string][]byte
	o := New(Deps{
		Wiki: "acme",
		KB:   fakeKB{commit: "abc"},
		Specs: fakeSpecs{items: []specs.Spec{
			{ID: "page-a", Wiki: "acme", Page: "PageA", Kind: "projection"},
		}},
		WikiTarget: fakeWiki{},
		LLM:        fakeLLM{},
		Frontends:  reg,
		Backend:    fakeBackend{},
		OnRendered: func(id string, b []byte, _ ir.Document) error {
			if captured == nil {
				captured = map[string][]byte{}
			}
			captured[id] = b
			return nil
		},
	})

	rep, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Specs) != 1 {
		t.Fatalf("len(Specs) = %d, want 1", len(rep.Specs))
	}
	if rep.Specs[0].Status != reporter.StatusRendered {
		t.Errorf("status = %q, want %q", rep.Specs[0].Status, reporter.StatusRendered)
	}
	if rep.Specs[0].BlocksRegenerated != 1 {
		t.Errorf("BlocksRegenerated = %d, want 1", rep.Specs[0].BlocksRegenerated)
	}
	if !bytes.Contains(captured["page-a"], []byte("from page-a")) {
		t.Errorf("captured render does not contain expected content: %s", captured["page-a"])
	}
}

func TestRun_WiredPipeline_UnknownKind_FailsSpec(t *testing.T) {
	reg := frontends.NewRegistry()
	// Only projection registered.
	reg.Register(fakeFrontend{})

	o := New(Deps{
		Wiki: "acme",
		KB:   fakeKB{commit: "abc"},
		Specs: fakeSpecs{items: []specs.Spec{
			{ID: "x", Wiki: "acme", Page: "X", Kind: "editorial"},
		}},
		WikiTarget: fakeWiki{},
		LLM:        fakeLLM{},
		Frontends:  reg,
		Backend:    fakeBackend{},
	})

	rep, _ := o.Run(context.Background())
	if rep.Specs[0].Status != reporter.StatusFailed {
		t.Errorf("status = %q, want %q (no editorial frontend registered)", rep.Specs[0].Status, reporter.StatusFailed)
	}
}

func TestRun_RunStateCache_RoundTripsBotRevAcrossRuns(t *testing.T) {
	// Two consecutive runs: the second run must see the bot revision
	// the first one wrote. This proves the reconciler exits
	// first-render mode once the cache is wired.
	dir := t.TempDir()
	rs, err := runstate.Open(filepath.Join(dir, "rs.bolt"))
	if err != nil {
		t.Fatalf("runstate.Open: %v", err)
	}
	defer func() { _ = rs.Close() }()

	reg := frontends.NewRegistry()
	reg.Register(fakeFrontend{})

	// Wiki target must be shared across runs — that's the whole point
	// of the test (run 2 sees what run 1 wrote).
	wikiTarget := memory.New("User:Bot")

	build := func() *Orchestrator {
		return New(Deps{
			Wiki: "acme",
			KB:   fakeKB{commit: "abc"},
			Specs: fakeSpecs{items: []specs.Spec{
				{ID: "page-a", Wiki: "acme", Page: "PageA", Kind: "projection"},
			}},
			WikiTarget: wikiTarget,
			LLM:        fakeLLM{},
			Frontends:  reg,
			Backend:    fakeBackend{},
			RunState:   rs,
		})
	}

	rep1, err := build().Run(context.Background())
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	if len(rep1.Specs) != 1 || rep1.Specs[0].NewRevisionID == "" {
		t.Fatalf("expected first run to record a new revision; got %+v", rep1.Specs)
	}
	firstRev := rep1.Specs[0].NewRevisionID

	// Verify the cache learned the revision.
	st, ok, _ := rs.Get("page-a")
	if !ok || st.LastBotRevID != firstRev {
		t.Errorf("cache state after run 1 = (%+v, %v), want LastBotRevID=%q", st, ok, firstRev)
	}

	// Second run: same fake frontend produces same content as before
	// in this test, but the wiki sees a different revision. The
	// reconciler should now look up the cache, find firstRev, and
	// produce ActionNoOp because content equality.
	rep2, _ := build().Run(context.Background())
	if rep2.Specs[0].Status != reporter.StatusSkipped {
		t.Errorf("second run status = %q, want %q (cache should enable no-op detection)", rep2.Specs[0].Status, reporter.StatusSkipped)
	}
}

// diffableKB returns a synthetic diff between specific commits.
type diffableKB struct {
	commit  string
	diffMap map[string][]string // prevCommit → changed areas
}

func (d diffableKB) Pull(context.Context) (kb.Snapshot, error) {
	return kb.Snapshot{Commit: d.commit}, nil
}
func (d diffableKB) DiffSince(_ context.Context, prev string) ([]string, error) {
	if areas, ok := d.diffMap[prev]; ok {
		return areas, nil
	}
	return nil, nil // no changes
}
func (diffableKB) Whoami() string { return "diffable" }

func TestRun_DiffDriven_SkipsSpecWithUntouchedIncludes(t *testing.T) {
	// Setup: prior run rendered spec "vault-page" at commit "C0" and
	// "harbor-page" at commit "C0". Current commit is "C1"; the diff
	// from C0→C1 touched only the "vault" area. The harbor spec
	// should be skipped; vault should still render.
	dir := t.TempDir()
	rs, err := runstate.Open(filepath.Join(dir, "rs.bolt"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rs.Close() }()

	// Pre-populate cache: both specs have rendered at C0 previously.
	_ = rs.Set("vault-page", runstate.SpecState{LastBotRevID: "rev-v", LastKBCommit: "C0"})
	_ = rs.Set("harbor-page", runstate.SpecState{LastBotRevID: "rev-h", LastKBCommit: "C0"})

	reg := frontends.NewRegistry()
	reg.Register(fakeFrontend{})

	wikiTarget := memory.New("User:Bot")

	o := New(Deps{
		Wiki: "acme",
		KB: diffableKB{
			commit:  "C1",
			diffMap: map[string][]string{"C0": {"vault"}},
		},
		Specs: fakeSpecs{items: []specs.Spec{
			{ID: "vault-page", Wiki: "acme", Page: "VaultPage", Kind: "projection",
				Include: specs.IncludeFilter{Areas: []string{"vault"}}},
			{ID: "harbor-page", Wiki: "acme", Page: "HarborPage", Kind: "projection",
				Include: specs.IncludeFilter{Areas: []string{"harbor"}}},
		}},
		WikiTarget: wikiTarget,
		LLM:        fakeLLM{},
		Frontends:  reg,
		Backend:    fakeBackend{},
		RunState:   rs,
	})

	rep, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Specs) != 2 {
		t.Fatalf("len(Specs) = %d, want 2", len(rep.Specs))
	}
	byID := map[string]reporter.SpecResult{}
	for _, s := range rep.Specs {
		byID[s.ID] = s
	}
	if byID["vault-page"].Status != reporter.StatusRendered {
		t.Errorf("vault-page status = %q, want %q (diff touches its area)", byID["vault-page"].Status, reporter.StatusRendered)
	}
	if byID["harbor-page"].Status != reporter.StatusSkipped {
		t.Errorf("harbor-page status = %q, want %q (diff does not touch its area)", byID["harbor-page"].Status, reporter.StatusSkipped)
	}
}

func TestRun_DiffDriven_FirstRender_AlwaysRenders(t *testing.T) {
	// Empty cache: no prior state for this spec. Diff-driven skip
	// must NOT apply — spec has never rendered before.
	dir := t.TempDir()
	rs, _ := runstate.Open(filepath.Join(dir, "rs.bolt"))
	defer func() { _ = rs.Close() }()

	reg := frontends.NewRegistry()
	reg.Register(fakeFrontend{})

	o := New(Deps{
		Wiki:       "acme",
		KB:         diffableKB{commit: "C1", diffMap: map[string][]string{"C0": {"vault"}}},
		Specs:      fakeSpecs{items: []specs.Spec{{ID: "new", Wiki: "acme", Page: "P", Kind: "projection", Include: specs.IncludeFilter{Areas: []string{"harbor"}}}}},
		WikiTarget: memory.New("U"),
		LLM:        fakeLLM{},
		Frontends:  reg,
		Backend:    fakeBackend{},
		RunState:   rs,
	})

	rep, _ := o.Run(context.Background())
	if rep.Specs[0].Status != reporter.StatusRendered {
		t.Errorf("first-render spec status = %q, want %q (no cache → must render)", rep.Specs[0].Status, reporter.StatusRendered)
	}
}

func TestRun_DiffDriven_KBUnchanged_SkipsAllPriorRenders(t *testing.T) {
	// Cache says spec rendered at C1 last time; current commit is also
	// C1. Must skip — no kb change at all.
	dir := t.TempDir()
	rs, _ := runstate.Open(filepath.Join(dir, "rs.bolt"))
	defer func() { _ = rs.Close() }()
	_ = rs.Set("p", runstate.SpecState{LastBotRevID: "r", LastKBCommit: "C1"})

	reg := frontends.NewRegistry()
	reg.Register(fakeFrontend{})

	o := New(Deps{
		Wiki:       "acme",
		KB:         diffableKB{commit: "C1"},
		Specs:      fakeSpecs{items: []specs.Spec{{ID: "p", Wiki: "acme", Page: "P", Kind: "projection", Include: specs.IncludeFilter{Areas: []string{"vault"}}}}},
		WikiTarget: memory.New("U"),
		LLM:        fakeLLM{},
		Frontends:  reg,
		Backend:    fakeBackend{},
		RunState:   rs,
	})

	rep, _ := o.Run(context.Background())
	if rep.Specs[0].Status != reporter.StatusSkipped {
		t.Errorf("status = %q, want %q (kb commit unchanged)", rep.Specs[0].Status, reporter.StatusSkipped)
	}
}

// countingFrontend records how many times Build was called. Used to
// prove the IR cache short-circuits.
type countingFrontend struct {
	calls int
}

func (c *countingFrontend) Name() string { return "counting" }
func (c *countingFrontend) Kind() string { return "projection" }
func (c *countingFrontend) Build(_ context.Context, s specs.Spec, _ kb.Snapshot) (ir.Document, error) {
	c.calls++
	return ir.Document{
		Frontmatter: ir.Frontmatter{Title: s.Page, SpecHash: s.Hash},
		Sections:    []ir.Section{{Heading: "S", Blocks: []ir.Block{ir.ProseBlock{Text: "x"}}}},
	}, nil
}

func TestRun_IRCache_SkipsFrontendOnHit(t *testing.T) {
	dir := t.TempDir()
	cache, err := ircache.Open(filepath.Join(dir, "ir"))
	if err != nil {
		t.Fatalf("ircache.Open: %v", err)
	}

	front := &countingFrontend{}
	reg := frontends.NewRegistry()
	reg.Register(front)

	build := func() *Orchestrator {
		return New(Deps{
			Wiki: "acme",
			KB:   fakeKB{commit: "abc"},
			Specs: fakeSpecs{items: []specs.Spec{
				{ID: "p", Wiki: "acme", Page: "P", Kind: "projection", Hash: "spec-hash-v1"},
			}},
			WikiTarget: fakeWiki{},
			LLM:        fakeLLM{},
			Frontends:  reg,
			Backend:    fakeBackend{},
			IRCache:    cache,
		})
	}

	_, _ = build().Run(context.Background())
	if front.calls != 1 {
		t.Fatalf("first run frontend calls = %d, want 1 (cache miss)", front.calls)
	}

	_, _ = build().Run(context.Background())
	if front.calls != 1 {
		t.Errorf("second run frontend calls = %d, want still 1 (cache hit)", front.calls)
	}
}

func TestRun_IRCache_DifferentSpecHashes_BothCallFrontend(t *testing.T) {
	dir := t.TempDir()
	cache, _ := ircache.Open(filepath.Join(dir, "ir"))

	front := &countingFrontend{}
	reg := frontends.NewRegistry()
	reg.Register(front)

	o := New(Deps{
		Wiki: "acme",
		KB:   fakeKB{commit: "abc"},
		Specs: fakeSpecs{items: []specs.Spec{
			{ID: "a", Wiki: "acme", Page: "A", Kind: "projection", Hash: "h1"},
			{ID: "b", Wiki: "acme", Page: "B", Kind: "projection", Hash: "h2"},
		}},
		WikiTarget: fakeWiki{},
		LLM:        fakeLLM{},
		Frontends:  reg,
		Backend:    fakeBackend{},
		IRCache:    cache,
	})
	_, _ = o.Run(context.Background())
	if front.calls != 2 {
		t.Errorf("calls = %d, want 2 (distinct spec hashes are distinct cache keys)", front.calls)
	}
}

func TestRun_IRCache_DisabledWithoutSpecHash(t *testing.T) {
	dir := t.TempDir()
	cache, _ := ircache.Open(filepath.Join(dir, "ir"))

	front := &countingFrontend{}
	reg := frontends.NewRegistry()
	reg.Register(front)

	build := func() *Orchestrator {
		return New(Deps{
			Wiki: "acme",
			KB:   fakeKB{commit: "abc"},
			Specs: fakeSpecs{items: []specs.Spec{
				// Hash empty — cache must be bypassed (otherwise specs
				// pre-hash would all collide under the empty key).
				{ID: "p", Wiki: "acme", Page: "P", Kind: "projection"},
			}},
			WikiTarget: fakeWiki{},
			LLM:        fakeLLM{},
			Frontends:  reg,
			Backend:    fakeBackend{},
			IRCache:    cache,
		})
	}

	_, _ = build().Run(context.Background())
	_, _ = build().Run(context.Background())
	if front.calls != 2 {
		t.Errorf("calls = %d, want 2 (empty spec.Hash should bypass the cache)", front.calls)
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
