package renderdiagrams

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var reMermaidSubgraph = regexp.MustCompile(`^(\s*subgraph\s+)(\S.*?)\s*$`)

// SanitizeMermaid conservatively repairs the two mermaid syntax
// mistakes LLM-authored diagrams hit most often, so a slightly-off
// diagram still renders instead of degrading to a <pre> block:
//
//   - Backticks in labels: LLMs code-format words (e.g. an overlay
//     network name) inside node labels; backticks are not valid in
//     plain flowchart/sequence labels and make mmdc fail. Stripped.
//   - subgraph titles containing parentheses: `subgraph Foo (Bar)`
//     fails to parse; mermaid needs the title quoted. Quoted (unless
//     it is already quoted, an id-only token, or the `id [Title]`
//     form).
//
// Deliberately narrow + idempotent: it only touches these two known
// failure classes, so already-valid diagrams pass through byte-for-
// byte. Add further repairs only as new failure classes are observed.
func SanitizeMermaid(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		m := reMermaidSubgraph.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		title := m[2]
		if strings.HasPrefix(title, `"`) || strings.Contains(title, "[") {
			continue // already quoted, or the `subgraph id [Title]` form
		}
		if strings.ContainsAny(title, "()") {
			lines[i] = m[1] + `"` + title + `"`
		}
	}
	return strings.ReplaceAll(strings.Join(lines, "\n"), "`", "")
}

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

	// PuppeteerConfig, when non-empty, is passed to mmdc as
	// `-p <path>`. In a container mmdc's headless Chrome must run
	// with --no-sandbox (a puppeteer config JSON); without it Chrome
	// fails to launch and every mermaid render errors. Defaults from
	// the MMDC_PUPPETEER_CONFIG env var (set by the runtime image).
	PuppeteerConfig string
}

// NewMermaidRenderer constructs a MermaidRenderer using the given
// mmdc binary path; empty means "mmdc" on PATH. The puppeteer config
// is taken from MMDC_PUPPETEER_CONFIG if set.
func NewMermaidRenderer(bin string) *MermaidRenderer {
	return NewMermaidRendererWithConfig(bin, os.Getenv("MMDC_PUPPETEER_CONFIG"))
}

// NewMermaidRendererWithConfig is the full-control constructor.
func NewMermaidRendererWithConfig(bin, puppeteerConfig string) *MermaidRenderer {
	if bin == "" {
		bin = "mmdc"
	}
	return &MermaidRenderer{Bin: bin, PuppeteerConfig: puppeteerConfig}
}

// MmdcArgs builds the mmdc argument vector. Exported for testing the
// flag construction without executing mmdc.
func (m *MermaidRenderer) MmdcArgs(in, out string) []string {
	args := []string{"-i", in, "-o", out, "-e", "png"}
	if m.PuppeteerConfig != "" {
		args = append(args, "-p", m.PuppeteerConfig)
	}
	return args
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
	// Conservatively repair the syntax mistakes LLMs make most often
	// so a slightly-off diagram renders instead of degrading to a
	// code block.
	if err := os.WriteFile(in, []byte(SanitizeMermaid(source)), 0o600); err != nil {
		return nil, "", fmt.Errorf("mmdc: write source: %w", err)
	}

	cmd := exec.CommandContext(ctx, m.Bin, m.MmdcArgs(in, out)...)
	if combined, err := cmd.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("mmdc: run: %w; output=%s", err, combined)
	}

	png, err := os.ReadFile(out)
	if err != nil {
		return nil, "", fmt.Errorf("mmdc: read output: %w", err)
	}
	return png, "image/png", nil
}
