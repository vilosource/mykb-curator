package renderdiagrams_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/renderdiagrams"
)

func TestMermaidRenderer_UnsupportedLang(t *testing.T) {
	// Deterministic and mmdc-independent: a non-mermaid language must
	// always take the escape-hatch path.
	r := renderdiagrams.NewMermaidRenderer("")
	_, _, err := r.Render(context.Background(), "plantuml", "@startuml\n@enduml")
	if !errors.Is(err, renderdiagrams.ErrUnsupportedLang) {
		t.Fatalf("Render(plantuml) error = %v, want ErrUnsupportedLang", err)
	}
}

func TestMermaidRenderer_RealRender(t *testing.T) {
	// Environment-gated: exercises the real mmdc subprocess only where
	// mmdc is installed (the curator container / nightly). Skipped —
	// not failed — elsewhere, and reported as a deferral.
	if _, err := exec.LookPath("mmdc"); err != nil {
		t.Skip("mmdc not on PATH; real mermaid render exercised only where mmdc is installed (curator container / nightly)")
	}
	r := renderdiagrams.NewMermaidRenderer("")
	img, ctype, err := r.Render(context.Background(), "mermaid", "graph TD; A-->B")
	if err != nil {
		t.Fatalf("Render(mermaid): %v", err)
	}
	if ctype != "image/png" {
		t.Errorf("contentType = %q, want image/png", ctype)
	}
	if len(img) < 8 || string(img[1:4]) != "PNG" {
		t.Errorf("output is not a PNG (len=%d, magic=%q)", len(img), img[:min(8, len(img))])
	}
}
