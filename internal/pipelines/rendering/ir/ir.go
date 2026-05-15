// Package ir defines the curator's intermediate representation —
// the PageDoc tree that frontends produce, passes transform, and
// backends render to a target format.
//
// Every IR block carries provenance metadata so the reconciler can
// distinguish machine-owned vs editorial regions and detect drift.
package ir

import "time"

// Document is the top-level IR for a single wiki page.
type Document struct {
	Frontmatter Frontmatter
	Sections    []Section
	Footer      Footer
}

// Frontmatter captures the document's identity and provenance.
type Frontmatter struct {
	Title       string
	SpecHash    string
	KBCommit    string
	GeneratedAt time.Time
}

// Section is one labelled subdivision of a Document.
type Section struct {
	Heading string
	Blocks  []Block
}

// Footer is rendered at the end of every page; includes the
// last-curated stamp, run-id, kb-commit, etc.
type Footer struct {
	LastCurated time.Time
	RunID       string
	KBCommit    string
}

// Block is the discriminated union over IR block kinds. The
// reconciler treats blocks differently based on Zone:
//   - ZoneMachine: always regenerated wholesale
//   - ZoneEditorial: preserved if input hash unchanged
type Block interface {
	Kind() string
	Zone() Zone
	Provenance() Provenance
}

// Zone determines reconciliation behaviour.
type Zone int

const (
	ZoneMachine   Zone = iota // always regenerated
	ZoneEditorial             // preserved if inputs unchanged
)

// Provenance is the per-block metadata that lets the reconciler
// detect drift and the cache key blocks correctly.
type Provenance struct {
	// SpecSection identifies the spec construct that produced this
	// block (e.g. "section[2].cover[3]").
	SpecSection string

	// Sources lists the kb entries that fed into this block. Each
	// item is a kb-ref like "area/vault/fact/abc123".
	Sources []string

	// InputHash is the hash of the (spec section + source contents)
	// at render time. Compared on subsequent runs to detect
	// material change.
	InputHash string
}

// ProseBlock is a narrative paragraph — editorial by default.
type ProseBlock struct {
	Text string
	Prov Provenance
}

func (ProseBlock) Kind() string             { return "prose" }
func (ProseBlock) Zone() Zone               { return ZoneEditorial }
func (b ProseBlock) Provenance() Provenance { return b.Prov }

// MachineBlock is auto-generated structural content — always
// regenerated, never preserved across runs.
type MachineBlock struct {
	BlockID string // stable ID used for CURATOR:BEGIN/END markers
	Kind_   string // sub-kind (e.g., "rg-table", "area-list")
	Body    string // pre-rendered body in IR-text form
	Prov    Provenance
}

func (MachineBlock) Kind() string             { return "machine" }
func (MachineBlock) Zone() Zone               { return ZoneMachine }
func (b MachineBlock) Provenance() Provenance { return b.Prov }

// KBRefBlock references a specific kb entry. Resolved by the
// ResolveKBRefs pass.
type KBRefBlock struct {
	Area string
	ID   string
	Mode string // "inline" | "footnote" | "link"
	Prov Provenance
}

func (KBRefBlock) Kind() string             { return "kbref" }
func (KBRefBlock) Zone() Zone               { return ZoneMachine }
func (b KBRefBlock) Provenance() Provenance { return b.Prov }

// TableBlock is a structured table generated from a kb query.
type TableBlock struct {
	Columns []string
	Rows    [][]string
	Prov    Provenance
}

func (TableBlock) Kind() string             { return "table" }
func (TableBlock) Zone() Zone               { return ZoneMachine }
func (b TableBlock) Provenance() Provenance { return b.Prov }

// DiagramBlock holds diagram source for rendering by the
// RenderDiagrams pass. Lang selects the renderer (mermaid is the
// default; plantuml, drawio handled by escape-hatch paths).
type DiagramBlock struct {
	Lang     string // "mermaid" | "plantuml" | "image-ref"
	Source   string
	AssetRef string // set after RenderDiagrams pass uploads the image
	Prov     Provenance
}

func (DiagramBlock) Kind() string             { return "diagram" }
func (DiagramBlock) Zone() Zone               { return ZoneMachine }
func (b DiagramBlock) Provenance() Provenance { return b.Prov }

// Callout is a note/warning/info box. Zone depends on origin: LLM
// prose is editorial; spec-fixed text is machine.
type Callout struct {
	Severity  string // "note" | "warning" | "info"
	Body      string
	IsMachine bool
	Prov      Provenance
}

func (Callout) Kind() string { return "callout" }
func (c Callout) Zone() Zone {
	if c.IsMachine {
		return ZoneMachine
	}
	return ZoneEditorial
}
func (c Callout) Provenance() Provenance { return c.Prov }

// EscapeHatch holds backend-specific raw markup. Lossy across
// backends; logged on every render so portability problems are
// visible.
type EscapeHatch struct {
	Backend string // "mediawiki" | "confluence" | ...
	Raw     string
	Prov    Provenance
}

func (EscapeHatch) Kind() string             { return "escape-hatch" }
func (EscapeHatch) Zone() Zone               { return ZoneMachine }
func (b EscapeHatch) Provenance() Provenance { return b.Prov }

// MarkerPosition indicates whether a MarkerBlock opens or closes a
// machine-owned region.
type MarkerPosition int

const (
	MarkerBegin MarkerPosition = iota
	MarkerEnd
)

// MarkerBlock is the IR representation of a zone-region boundary.
// Produced by the ApplyZoneMarkers pass; rendered by each backend
// using format-appropriate syntax (HTML comments for markdown /
// wikitext / Confluence; future backends may differ).
//
// Owning the marker as an IR-level block (instead of having backends
// emit markers themselves) means the marker convention is set in one
// place (the pass) and every backend renders mechanically.
type MarkerBlock struct {
	Position MarkerPosition
	BlockID  string // matches the block the marker brackets
	Prov     Provenance

	// OfZone describes what kind of block this marker brackets:
	// "machine" or "editorial". Surfaces in the wiki marker so the
	// reconciler — reading the wiki page back — knows whether to
	// preserve content (editorial + matching provenance) or
	// overwrite (machine, or different provenance).
	//
	// Distinct from MarkerBlock.Zone() — that returns the marker's
	// OWN zone (always machine; markers are never preserved). This
	// is the bracketed block's zone.
	OfZone string
}

func (MarkerBlock) Kind() string             { return "marker" }
func (MarkerBlock) Zone() Zone               { return ZoneMachine }
func (b MarkerBlock) Provenance() Provenance { return b.Prov }
