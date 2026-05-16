package renderdiagrams_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/renderdiagrams"
)

type fakeLLM struct {
	resp   string
	err    error
	gotReq llm.Request
}

func (f *fakeLLM) Complete(_ context.Context, r llm.Request) (llm.Response, error) {
	f.gotReq = r
	if f.err != nil {
		return llm.Response{}, f.err
	}
	return llm.Response{Text: f.resp}, nil
}

func TestLLMRepairer_ExtractsCleanDiagram(t *testing.T) {
	// The model wraps the fix in prose + a fenced block; the repairer
	// must return ONLY the diagram source.
	fl := &fakeLLM{resp: "Sure, here is the corrected diagram:\n\n```mermaid\ngraph TD\n  A-->B\n```\n"}
	r := renderdiagrams.NewLLMRepairer(fl, "m")
	got, err := r.Repair(context.Background(), "mermaid", "graph TD\n  A-->`B`", "mmdc: run: exit status 1")
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if got != "graph TD\n  A-->B" {
		t.Errorf("Repair returned %q, want clean fenced contents", got)
	}
	if !strings.Contains(fl.gotReq.Prompt, "graph TD") || !strings.Contains(fl.gotReq.Prompt, "exit status 1") {
		t.Errorf("prompt must include the bad source + the render error: %q", fl.gotReq.Prompt)
	}
}

func TestLLMRepairer_NoFence_ReturnsTrimmed(t *testing.T) {
	fl := &fakeLLM{resp: "  graph LR; X-->Y  \n"}
	got, err := renderdiagrams.NewLLMRepairer(fl, "m").Repair(context.Background(), "mermaid", "src", "err")
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if got != "graph LR; X-->Y" {
		t.Errorf("got %q", got)
	}
}

func TestLLMRepairer_LLMError_Propagates(t *testing.T) {
	_, err := renderdiagrams.NewLLMRepairer(&fakeLLM{err: errors.New("boom")}, "m").
		Repair(context.Background(), "mermaid", "s", "e")
	if err == nil {
		t.Errorf("LLM error must propagate (caller then degrades)")
	}
}

// fakeRenderer is a deterministic Renderer test double. It records
// the calls it received and returns canned bytes, or a configured
// error.
type fakeRenderer struct {
	img   []byte
	ctype string
	err   error
	calls []renderCall
}

type renderCall struct {
	lang, source string
}

func (f *fakeRenderer) Render(_ context.Context, lang, source string) ([]byte, string, error) {
	f.calls = append(f.calls, renderCall{lang, source})
	if f.err != nil {
		return nil, "", f.err
	}
	return f.img, f.ctype, nil
}

// fakeUploader records uploads and returns a deterministic ref.
type fakeUploader struct {
	calls []uploadCall
	err   error
}

type uploadCall struct {
	filename, ctype, summary string
	content                  []byte
}

func (f *fakeUploader) UploadFile(_ context.Context, filename string, content []byte, ctype, summary string) (string, error) {
	f.calls = append(f.calls, uploadCall{filename, ctype, summary, content})
	if f.err != nil {
		return "", f.err
	}
	return "asset://" + filename, nil
}

func docWith(blocks ...ir.Block) ir.Document {
	return ir.Document{Sections: []ir.Section{{Heading: "S", Blocks: blocks}}}
}

func firstBlock(t *testing.T, d ir.Document) ir.Block {
	t.Helper()
	if len(d.Sections) == 0 || len(d.Sections[0].Blocks) == 0 {
		t.Fatalf("document has no blocks")
	}
	return d.Sections[0].Blocks[0]
}

func TestName(t *testing.T) {
	p := renderdiagrams.New(&fakeRenderer{}, &fakeUploader{})
	if p.Name() != "render-diagrams" {
		t.Errorf("Name() = %q, want %q", p.Name(), "render-diagrams")
	}
}

func TestMermaidBlock_RenderedAndUploaded(t *testing.T) {
	r := &fakeRenderer{img: []byte("PNGDATA"), ctype: "image/png"}
	u := &fakeUploader{}
	p := renderdiagrams.New(r, u)

	in := docWith(ir.DiagramBlock{Lang: "mermaid", Source: "graph TD; A-->B"})
	out, err := p.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(r.calls) != 1 || r.calls[0].lang != "mermaid" || r.calls[0].source != "graph TD; A-->B" {
		t.Fatalf("renderer calls = %+v, want one mermaid call", r.calls)
	}
	if len(u.calls) != 1 {
		t.Fatalf("uploader calls = %d, want 1", len(u.calls))
	}
	if string(u.calls[0].content) != "PNGDATA" || u.calls[0].ctype != "image/png" {
		t.Errorf("upload content/ctype = %q/%q, want PNGDATA/image/png", u.calls[0].content, u.calls[0].ctype)
	}
	db, ok := firstBlock(t, out).(ir.DiagramBlock)
	if !ok {
		t.Fatalf("block is %T, want DiagramBlock", firstBlock(t, out))
	}
	if db.AssetRef != "asset://"+u.calls[0].filename {
		t.Errorf("AssetRef = %q, want %q", db.AssetRef, "asset://"+u.calls[0].filename)
	}
}

func TestDeterministicFilename(t *testing.T) {
	mk := func() *fakeUploader {
		u := &fakeUploader{}
		p := renderdiagrams.New(&fakeRenderer{img: []byte("X"), ctype: "image/png"}, u)
		_, err := p.Apply(context.Background(), docWith(ir.DiagramBlock{Lang: "mermaid", Source: "same source"}))
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		return u
	}
	a, b := mk(), mk()
	if a.calls[0].filename != b.calls[0].filename {
		t.Errorf("filenames not deterministic: %q vs %q", a.calls[0].filename, b.calls[0].filename)
	}
	// Different source must yield a different filename.
	u := &fakeUploader{}
	p := renderdiagrams.New(&fakeRenderer{img: []byte("X"), ctype: "image/png"}, u)
	if _, err := p.Apply(context.Background(), docWith(ir.DiagramBlock{Lang: "mermaid", Source: "different"})); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if u.calls[0].filename == a.calls[0].filename {
		t.Errorf("different sources produced same filename %q", u.calls[0].filename)
	}
}

func TestAlreadyRendered_Skipped(t *testing.T) {
	r := &fakeRenderer{img: []byte("X"), ctype: "image/png"}
	u := &fakeUploader{}
	p := renderdiagrams.New(r, u)

	in := docWith(ir.DiagramBlock{Lang: "mermaid", Source: "g", AssetRef: "asset://already.png"})
	out, err := p.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(r.calls) != 0 || len(u.calls) != 0 {
		t.Errorf("expected no render/upload for already-rendered block; render=%d upload=%d", len(r.calls), len(u.calls))
	}
	db := firstBlock(t, out).(ir.DiagramBlock)
	if db.AssetRef != "asset://already.png" {
		t.Errorf("AssetRef changed to %q, want preserved", db.AssetRef)
	}
}

func TestEmptySource_Passthrough(t *testing.T) {
	r := &fakeRenderer{}
	u := &fakeUploader{}
	p := renderdiagrams.New(r, u)

	out, err := p.Apply(context.Background(), docWith(ir.DiagramBlock{Lang: "mermaid", Source: ""}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(r.calls) != 0 || len(u.calls) != 0 {
		t.Errorf("empty source should be a no-op; render=%d upload=%d", len(r.calls), len(u.calls))
	}
	_ = firstBlock(t, out) // still present, unchanged
}

func TestUnsupportedLang_EscapeHatchPassthrough(t *testing.T) {
	r := &fakeRenderer{err: renderdiagrams.ErrUnsupportedLang}
	u := &fakeUploader{}
	p := renderdiagrams.New(r, u)

	in := docWith(ir.DiagramBlock{Lang: "plantuml", Source: "@startuml\n@enduml"})
	out, err := p.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("unsupported lang must not fail the pipeline: %v", err)
	}
	if len(u.calls) != 0 {
		t.Errorf("nothing should be uploaded for unsupported lang")
	}
	db := firstBlock(t, out).(ir.DiagramBlock)
	if db.AssetRef != "" || db.Source != "@startuml\n@enduml" {
		t.Errorf("unsupported block must pass through unchanged, got %+v", db)
	}
}

// A render failure (e.g. mmdc exit 1 on malformed LLM-authored
// mermaid) must DEGRADE, not abort the page: the block keeps its
// source (backend renders it as a code block) and the pass returns
// no error. A single bad diagram nuking an otherwise-good page is
// unacceptable for agent-generated content.
func TestRendererError_DegradesNotPropagates(t *testing.T) {
	boom := errors.New("mmdc: run: exit status 1")
	u := &fakeUploader{}
	p := renderdiagrams.New(&fakeRenderer{err: boom}, u)
	out, err := p.Apply(context.Background(), docWith(ir.DiagramBlock{Lang: "mermaid", Source: "graph oops"}))
	if err != nil {
		t.Fatalf("render failure must not fail the pipeline, got %v", err)
	}
	if len(u.calls) != 0 {
		t.Errorf("a failed render must not upload anything")
	}
	db := firstBlock(t, out).(ir.DiagramBlock)
	if db.AssetRef != "" || db.Source != "graph oops" {
		t.Errorf("failed-render block must pass through with source intact, got %+v", db)
	}
}

// fakeRepairer returns a canned "fixed" source and records the call.
type fakeRepairer struct {
	fixed     string
	err       error
	gotErr    string
	gotSource string
	calls     int
}

func (f *fakeRepairer) Repair(_ context.Context, _, source, renderErr string) (string, error) {
	f.calls++
	f.gotSource, f.gotErr = source, renderErr
	return f.fixed, f.err
}

// renderer that fails on the original source but succeeds on the
// repaired one — models mmdc rejecting bad LLM mermaid then
// accepting the repaired version.
type repairableRenderer struct {
	bad, good string
	calls     []string
}

func (r *repairableRenderer) Render(_ context.Context, _, source string) ([]byte, string, error) {
	r.calls = append(r.calls, source)
	if source == r.good {
		return []byte("PNG"), "image/png", nil
	}
	return nil, "", errors.New("mmdc: run: exit status 1")
}

func TestRepair_FixesAndRenders(t *testing.T) {
	rr := &repairableRenderer{bad: "graph bad", good: "graph good"}
	u := &fakeUploader{}
	rep := &fakeRepairer{fixed: "graph good"}
	p := renderdiagrams.NewWithRepairer(rr, u, rep)

	out, err := p.Apply(context.Background(), docWith(ir.DiagramBlock{Lang: "mermaid", Source: "graph bad"}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if rep.calls != 1 || rep.gotSource != "graph bad" || rep.gotErr == "" {
		t.Errorf("repairer not called with (bad source, render error): %+v", rep)
	}
	if len(rr.calls) != 2 {
		t.Fatalf("expected 2 render attempts (original, repaired), got %v", rr.calls)
	}
	db := firstBlock(t, out).(ir.DiagramBlock)
	if db.AssetRef == "" || len(u.calls) != 1 {
		t.Errorf("repaired diagram should upload + get an AssetRef, got %+v / uploads=%d", db, len(u.calls))
	}
	// Filename keys on the ORIGINAL source so re-runs stay idempotent
	// despite non-deterministic repair output: a second Apply of the
	// same original must hit the same filename.
	u2 := &fakeUploader{}
	p2 := renderdiagrams.NewWithRepairer(&repairableRenderer{bad: "graph bad", good: "graph good"}, u2, &fakeRepairer{fixed: "graph good"})
	if _, err := p2.Apply(context.Background(), docWith(ir.DiagramBlock{Lang: "mermaid", Source: "graph bad"})); err != nil {
		t.Fatalf("Apply #2: %v", err)
	}
	if u.calls[0].filename != u2.calls[0].filename {
		t.Errorf("filename not stable on original source: %q vs %q", u.calls[0].filename, u2.calls[0].filename)
	}
}

func TestRepair_StillBad_Degrades(t *testing.T) {
	rr := &repairableRenderer{bad: "graph bad", good: "never"}
	u := &fakeUploader{}
	rep := &fakeRepairer{fixed: "still bad"} // repaired source still won't render
	p := renderdiagrams.NewWithRepairer(rr, u, rep)

	out, err := p.Apply(context.Background(), docWith(ir.DiagramBlock{Lang: "mermaid", Source: "graph bad"}))
	if err != nil {
		t.Fatalf("Apply must not fail the pipeline: %v", err)
	}
	db := firstBlock(t, out).(ir.DiagramBlock)
	if db.AssetRef != "" || db.Source != "graph bad" || len(u.calls) != 0 {
		t.Errorf("still-bad repair must degrade with original source intact, got %+v", db)
	}
}

func TestRepair_NotAttemptedForUnsupportedLang(t *testing.T) {
	rep := &fakeRepairer{fixed: "x"}
	p := renderdiagrams.NewWithRepairer(&fakeRenderer{err: renderdiagrams.ErrUnsupportedLang}, &fakeUploader{}, rep)
	if _, err := p.Apply(context.Background(), docWith(ir.DiagramBlock{Lang: "plantuml", Source: "@startuml"})); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if rep.calls != 0 {
		t.Errorf("repair must NOT run for unsupported languages (not a fixable mermaid error)")
	}
}

func TestUploaderError_Propagates(t *testing.T) {
	boom := errors.New("upload 500")
	p := renderdiagrams.New(&fakeRenderer{img: []byte("X"), ctype: "image/png"}, &fakeUploader{err: boom})
	_, err := p.Apply(context.Background(), docWith(ir.DiagramBlock{Lang: "mermaid", Source: "g"}))
	if !errors.Is(err, boom) {
		t.Errorf("expected wrapped uploader error, got %v", err)
	}
}

func TestNonDiagramBlocks_Untouched(t *testing.T) {
	r := &fakeRenderer{}
	u := &fakeUploader{}
	p := renderdiagrams.New(r, u)

	in := docWith(ir.ProseBlock{Text: "hello"}, ir.MachineBlock{BlockID: "b1", Body: "x"})
	out, err := p.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(r.calls) != 0 || len(u.calls) != 0 {
		t.Errorf("non-diagram blocks must not trigger render/upload")
	}
	if len(out.Sections[0].Blocks) != 2 {
		t.Errorf("blocks lost: %+v", out.Sections[0].Blocks)
	}
}
