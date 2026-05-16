// Package renderdiagrams implements the RenderDiagrams pass
// (DESIGN.md §5.4).
//
// For every DiagramBlock carrying renderable source, the pass:
//   - renders the source to an image via an injected Renderer
//     (mermaid is first-class; the real renderer shells out to
//     `mmdc` — kept behind the interface so this pass stays
//     deterministic and unit-testable, mirroring how the editorial
//     frontend injects its LLM client),
//   - uploads the image via an injected Uploader (satisfied by
//     wiki.Target — see DESIGN §17 "upload via wiki adapter's
//     UploadFile"),
//   - records the returned asset reference on the block so the
//     backend can embed it.
//
// Determinism: the pass itself is a pure orchestration over its two
// injected collaborators. The uploaded filename is derived from a
// content hash of (lang, source), so identical diagrams always land
// at the same wiki filename — making the upload idempotent at the
// wiki level ("upload only if source changed", DESIGN §5.3 table).
//
// Idempotent within the pipeline: a DiagramBlock that already has an
// AssetRef is left untouched (it was rendered on a prior pass/run; a
// source change invalidates the IR-cache key and produces a fresh
// block with an empty AssetRef).
//
// Languages other than the ones the Renderer supports follow the
// escape-hatch path: the Renderer returns ErrUnsupportedLang and the
// block passes through unchanged (raw source preserved) rather than
// failing the run.
package renderdiagrams

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// ErrUnsupportedLang is returned by a Renderer for a diagram language
// it cannot render. The pass treats it as the escape-hatch signal:
// leave the block as-is, do not fail the pipeline.
var ErrUnsupportedLang = errors.New("renderdiagrams: unsupported diagram language")

// Renderer turns diagram source into image bytes. Injected so the
// pass stays deterministic and the mmdc subprocess stays out of unit
// tests (same pattern as the editorial frontend's LLM client).
type Renderer interface {
	// Render renders source written in lang to image bytes plus the
	// image's content type (e.g. "image/png"). It must return
	// ErrUnsupportedLang for languages it does not handle so the pass
	// can take the escape-hatch path.
	Render(ctx context.Context, lang, source string) (img []byte, contentType string, err error)
}

// Uploader uploads an image asset to the wiki and returns the
// reference string a backend embeds. Deliberately narrow (interface
// segregation): wiki.Target satisfies it without this pass depending
// on the whole wiki surface.
type Uploader interface {
	UploadFile(ctx context.Context, filename string, content []byte, contentType, summary string) (assetRef string, err error)
}

// RenderDiagrams is the Pass implementation.
type RenderDiagrams struct {
	r Renderer
	u Uploader
}

// New constructs a RenderDiagrams pass over the given renderer and
// uploader.
func New(r Renderer, u Uploader) *RenderDiagrams { return &RenderDiagrams{r: r, u: u} }

// Name returns "render-diagrams".
func (*RenderDiagrams) Name() string { return "render-diagrams" }

// Apply renders + uploads every renderable DiagramBlock and stamps
// its AssetRef. Non-DiagramBlocks and already-rendered / empty /
// unsupported diagrams pass through unchanged.
func (p *RenderDiagrams) Apply(ctx context.Context, doc ir.Document) (ir.Document, error) {
	for si := range doc.Sections {
		blocks := doc.Sections[si].Blocks
		for bi := range blocks {
			db, ok := blocks[bi].(ir.DiagramBlock)
			if !ok {
				continue
			}
			updated, err := p.renderOne(ctx, db)
			if err != nil {
				return doc, err
			}
			blocks[bi] = updated
		}
	}
	return doc, nil
}

// renderOne handles a single DiagramBlock. It returns the block
// unchanged for the no-op cases (empty source, already rendered,
// unsupported language) and a hard error only for genuine
// render/upload failures.
func (p *RenderDiagrams) renderOne(ctx context.Context, db ir.DiagramBlock) (ir.DiagramBlock, error) {
	if db.Source == "" || db.AssetRef != "" {
		return db, nil
	}

	img, ctype, err := p.r.Render(ctx, db.Lang, db.Source)
	if err != nil {
		// Any render failure degrades to the escape-hatch: keep the
		// source so the backend renders it as a code block, and
		// continue. This covers both unsupported languages and
		// malformed source — LLM-authored mermaid is frequently
		// imperfect, and a single bad diagram must never abort an
		// otherwise-good page. (Upload failures below are still
		// hard: those mean the wiki target itself is broken, which
		// would fail the page write anyway.)
		return db, nil
	}

	filename := assetFilename(db.Lang, db.Source)
	summary := "curator: render diagram"
	if db.Prov.SpecSection != "" {
		summary += " for " + db.Prov.SpecSection
	}
	ref, err := p.u.UploadFile(ctx, filename, img, ctype, summary)
	if err != nil {
		return db, fmt.Errorf("renderdiagrams: upload %q: %w", filename, err)
	}
	db.AssetRef = ref
	return db, nil
}

// assetFilename derives a stable upload filename from the diagram's
// language + source. Identical diagrams always map to the same name,
// so re-uploads of unchanged content are idempotent at the wiki.
func assetFilename(lang, source string) string {
	sum := sha256.Sum256([]byte(lang + "\x00" + source))
	return "diagram-" + hex.EncodeToString(sum[:])[:16] + ".png"
}
