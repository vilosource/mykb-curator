// Package specchat is the composition glue behind the spec-chat
// curator-api: the real Previewer (docspec.Parse -> cluster.Render ->
// markdown -> judge.Review) and the real KBWriter (shells the
// sanctioned stable `kb add`). This composition deliberately lives
// OUTSIDE internal/adapters/curatorapi so that the HTTP adapter holds
// zero domain logic (design D1/D3/D6).
package specchat

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/vilosource/mykb-curator/internal/adapters/curatorapi"
	kbpkg "github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/judge"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends/markdown"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/cluster"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/architecture"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// KBReader is the brain read side (kb.Source satisfies it).
type KBReader interface {
	Pull(ctx context.Context) (kbpkg.Snapshot, error)
}

// Previewer composes render + Judge on a candidate spec (design D3).
type Previewer struct {
	kb KBReader
	cl *cluster.Cluster
	j  *judge.Judge
	be *markdown.Backend
}

// NewPreviewer wires the composite previewer.
func NewPreviewer(kb KBReader, cl *cluster.Cluster, j *judge.Judge) *Previewer {
	return &Previewer{kb: kb, cl: cl, j: j, be: markdown.New()}
}

// Preview renders the candidate cluster and Judges every page against
// the SAME per-section grounding the synthesis was given — the
// fidelity property hardened in the converged Judge loop.
func (p *Previewer) Preview(ctx context.Context, candidate []byte) (curatorapi.PreviewResult, error) {
	var out curatorapi.PreviewResult
	spec, err := docspec.Parse(candidate)
	if err != nil {
		return out, fmt.Errorf("parse candidate: %w", err)
	}
	snap, err := p.kb.Pull(ctx)
	if err != nil {
		return out, fmt.Errorf("kb pull: %w", err)
	}
	rendered, err := p.cl.Render(ctx, spec, snap)
	if err != nil {
		return out, fmt.Errorf("render: %w", err)
	}
	dpages := append([]docspec.DocPage{spec.Parent}, spec.Children...)

	allPass := true
	for i, rp := range rendered {
		md, mErr := p.be.Render(rp.Doc)
		if mErr != nil {
			return out, fmt.Errorf("markdown %q: %w", rp.Page, mErr)
		}
		out.Pages = append(out.Pages, curatorapi.PreviewPage{Page: rp.Page, Markdown: string(md)})

		if i >= len(dpages) {
			continue
		}
		dp := dpages[i]
		grounding := make(map[string]string, len(dp.Sections))
		for _, sec := range dp.Sections {
			grounding[sec.Title] = architecture.SectionGrounding(snap, sec)
		}
		rep, jErr := p.j.Review(ctx, dp, rp.Doc, grounding)
		if jErr != nil {
			return out, fmt.Errorf("judge %q: %w", rp.Page, jErr)
		}
		for _, v := range rep.Verdicts {
			out.Verdicts = append(out.Verdicts, curatorapi.Verdict{
				Section: v.Section, Pass: v.Pass, Reason: v.Reason,
			})
			out.UngroundedClaims = append(out.UngroundedClaims, v.UngroundedClaims...)
		}
		if !rep.AllPass() {
			allPass = false
		}
	}
	out.AllPass = allPass
	return out, nil
}

// ShellKBWriter implements curatorapi.KBWriter by invoking the
// sanctioned stable `kb add` CLI (design D6: never direct JSONL,
// never kb-develop; provenance mandatory; lands in incoming/
// unverified — enforced by the adapter before this is reached).
type ShellKBWriter struct {
	// Bin is the kb binary (default "kb"); overridable for tests.
	Bin string
	// MYKBDir, if set, is exported as $MYKB_DIR so a test/secondary
	// brain can be targeted without touching the real ~/.mykb.
	MYKBDir string
}

// NewShellKBWriter constructs a writer against the `kb` on PATH.
func NewShellKBWriter() *ShellKBWriter { return &ShellKBWriter{Bin: "kb"} }

// AddEntry runs: kb add <type> <area> <text> --source <s> [--why <w>]
// and returns the new entry id parsed from stdout (best-effort: the
// entry is created regardless; an unparsed id is "" with nil error).
func (s *ShellKBWriter) AddEntry(ctx context.Context, area, typ, text, source, why string) (string, error) {
	bin := s.Bin
	if bin == "" {
		bin = "kb"
	}
	args := []string{"add", typ, area, text, "--source", source}
	if why != "" {
		args = append(args, "--why", why)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	if s.MYKBDir != "" {
		cmd.Env = append(cmd.Environ(), "MYKB_DIR="+s.MYKBDir)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("kb add: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseEntryID(stdout.String()), nil
}

// parseEntryID extracts an 8-char-ish id token from `kb add` output.
// Format is not contractual, so this is intentionally lenient: it
// looks for a bracketed or "id:"-prefixed token and otherwise yields
// "" (the caller treats id as advisory, not load-bearing).
func parseEntryID(out string) string {
	for _, f := range strings.Fields(out) {
		t := strings.Trim(f, "[]()'\".,")
		if strings.HasPrefix(f, "id:") {
			return strings.TrimPrefix(f, "id:")
		}
		if len(t) >= 6 && len(t) <= 16 && isIDish(t) {
			return t
		}
	}
	return ""
}

func isIDish(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
