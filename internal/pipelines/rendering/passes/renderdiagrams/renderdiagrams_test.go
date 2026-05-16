package renderdiagrams_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/renderdiagrams"
)

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

func TestRendererHardError_Propagates(t *testing.T) {
	boom := errors.New("mmdc exploded")
	p := renderdiagrams.New(&fakeRenderer{err: boom}, &fakeUploader{})
	_, err := p.Apply(context.Background(), docWith(ir.DiagramBlock{Lang: "mermaid", Source: "g"}))
	if !errors.Is(err, boom) {
		t.Errorf("expected wrapped renderer error, got %v", err)
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
