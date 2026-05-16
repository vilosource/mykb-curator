package renderdiagrams

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// MermaidRenderer is the production Renderer. It shells out to the
// mermaid CLI (`mmdc`) to turn mermaid source into a PNG.
//
// mmdc is a Node tool that drives a headless Chromium; it is not a
// Go dependency. The curator container ships with it (DESIGN.md
// §16). Keeping the subprocess behind the Renderer interface is what
// lets the RenderDiagrams pass stay deterministic + unit-testable
// without mmdc present.
//
// Only mermaid is first-class. Any other language returns
// ErrUnsupportedLang so the pass takes the escape-hatch path
// (DESIGN.md §16: "pre-rendered images as escape hatch for diagrams
// mermaid can't express").
type MermaidRenderer struct {
	// Bin is the mmdc binary name/path. Defaults to "mmdc".
	Bin string
}

// NewMermaidRenderer constructs a MermaidRenderer using the given
// mmdc binary path; empty means "mmdc" on PATH.
func NewMermaidRenderer(bin string) *MermaidRenderer {
	if bin == "" {
		bin = "mmdc"
	}
	return &MermaidRenderer{Bin: bin}
}

// Render writes source to a temp .mmd file, runs mmdc to produce a
// PNG, and returns the PNG bytes. Languages other than "mermaid"
// (and the empty default) yield ErrUnsupportedLang.
func (m *MermaidRenderer) Render(ctx context.Context, lang, source string) ([]byte, string, error) {
	if lang != "" && lang != "mermaid" {
		return nil, "", fmt.Errorf("%w: %q", ErrUnsupportedLang, lang)
	}

	dir, err := os.MkdirTemp("", "mykb-curator-mmdc-*")
	if err != nil {
		return nil, "", fmt.Errorf("mmdc: tempdir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	in := filepath.Join(dir, "diagram.mmd")
	out := filepath.Join(dir, "diagram.png")
	if err := os.WriteFile(in, []byte(source), 0o600); err != nil {
		return nil, "", fmt.Errorf("mmdc: write source: %w", err)
	}

	cmd := exec.CommandContext(ctx, m.Bin, "-i", in, "-o", out, "-e", "png")
	if combined, err := cmd.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("mmdc: run: %w; output=%s", err, combined)
	}

	png, err := os.ReadFile(out)
	if err != nil {
		return nil, "", fmt.Errorf("mmdc: read output: %w", err)
	}
	return png, "image/png", nil
}
