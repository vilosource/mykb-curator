package passes

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// fakePass tags every prose block's Text with its name. Pure, no I/O.
// Used to assert pipeline ordering: after Apply, prose text reads
// "first<original>second<original>..." for each pass that ran.
type fakePass struct {
	name string
	err  error
}

func (f fakePass) Name() string { return f.name }

func (f fakePass) Apply(_ context.Context, doc ir.Document) (ir.Document, error) {
	if f.err != nil {
		return doc, f.err
	}
	for i, sec := range doc.Sections {
		for j, b := range sec.Blocks {
			if p, ok := b.(ir.ProseBlock); ok {
				p.Text = f.name + ":" + p.Text
				doc.Sections[i].Blocks[j] = p
			}
		}
	}
	return doc, nil
}

func proseDoc(text string) ir.Document {
	return ir.Document{
		Sections: []ir.Section{{
			Heading: "S",
			Blocks:  []ir.Block{ir.ProseBlock{Text: text}},
		}},
	}
}

func TestPipeline_EmptyIsNoOp(t *testing.T) {
	p := NewPipeline()
	in := proseDoc("hello")
	out, err := p.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := out.Sections[0].Blocks[0].(ir.ProseBlock).Text
	if got != "hello" {
		t.Errorf("text = %q, want unchanged %q", got, "hello")
	}
}

func TestPipeline_RunsPassesInOrder(t *testing.T) {
	p := NewPipeline(fakePass{name: "first"}, fakePass{name: "second"}, fakePass{name: "third"})
	out, err := p.Apply(context.Background(), proseDoc("x"))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := out.Sections[0].Blocks[0].(ir.ProseBlock).Text
	want := "third:second:first:x"
	if got != want {
		t.Errorf("text = %q, want %q (passes must run first→second→third)", got, want)
	}
}

func TestPipeline_StopsOnFirstError(t *testing.T) {
	wantErr := errors.New("simulated")
	p := NewPipeline(
		fakePass{name: "ok1"},
		fakePass{name: "boom", err: wantErr},
		fakePass{name: "should-not-run"},
	)
	_, err := p.Apply(context.Background(), proseDoc("x"))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wraps %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %q, want failing pass name 'boom' in message", err)
	}
}

func TestPipeline_Names_ReturnsOrderedSlice(t *testing.T) {
	p := NewPipeline(fakePass{name: "a"}, fakePass{name: "b"}, fakePass{name: "c"})
	names := p.Names()
	want := []string{"a", "b", "c"}
	if len(names) != len(want) {
		t.Fatalf("len(Names) = %d, want %d", len(names), len(want))
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("Names[%d] = %q, want %q", i, names[i], n)
		}
	}
}
