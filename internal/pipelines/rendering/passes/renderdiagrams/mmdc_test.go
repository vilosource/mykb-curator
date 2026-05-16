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

func TestMermaidRenderer_PuppeteerConfigArg(t *testing.T) {
	// In a container, mmdc's headless Chrome needs --no-sandbox via a
	// puppeteer config file. The renderer must pass `-p <cfg>` when
	// configured, and omit it otherwise.
	with := renderdiagrams.NewMermaidRendererWithConfig("mmdc", "/etc/mmdc-pptr.json")
	args := with.MmdcArgs("in.mmd", "out.png")
	if !contains(args, "-p") || !contains(args, "/etc/mmdc-pptr.json") {
		t.Errorf("args %v missing -p /etc/mmdc-pptr.json", args)
	}
	without := renderdiagrams.NewMermaidRenderer("")
	if contains(without.MmdcArgs("in.mmd", "out.png"), "-p") {
		t.Errorf("no puppeteer config ⇒ no -p flag; got %v", without.MmdcArgs("in.mmd", "out.png"))
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestSanitizeMermaid(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			"backticks in label removed",
			"graph TD\n  OV(Swarm Overlay Network `infra-net`)",
			"graph TD\n  OV(Swarm Overlay Network infra-net)",
		},
		{
			"subgraph title with parens quoted",
			"subgraph Vault Cluster (Raft HA)\nend",
			"subgraph \"Vault Cluster (Raft HA)\"\nend",
		},
		{
			"already-quoted subgraph untouched",
			"subgraph \"X (Y)\"\nend",
			"subgraph \"X (Y)\"\nend",
		},
		{
			"plain subgraph untouched",
			"subgraph Clients\nend",
			"subgraph Clients\nend",
		},
		{
			"valid diagram unchanged",
			"graph TD; A-->B",
			"graph TD; A-->B",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renderdiagrams.SanitizeMermaid(c.in); got != c.want {
				t.Errorf("SanitizeMermaid(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
	// Idempotent + deterministic.
	once := renderdiagrams.SanitizeMermaid("subgraph A (b)\nX(`y`)")
	if renderdiagrams.SanitizeMermaid(once) != once {
		t.Errorf("SanitizeMermaid not idempotent")
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
