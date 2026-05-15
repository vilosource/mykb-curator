// Package wikiparse extracts CURATOR:BEGIN/END marked blocks from a
// page body (markdown, wikitext, Confluence storage — all share the
// HTML-comment marker syntax).
//
// Inputs are treated as opaque text outside markers. Anything before
// the first BEGIN is the Prologue; anything after the last END is
// the Epilogue; text between an END and the next BEGIN becomes the
// preceding block's FollowingText.
//
// Malformed input (unclosed BEGIN, mismatched IDs) yields fewer
// blocks but never panics — the reconciler treats unparseable
// content as "no recoverable prior state" and overwrites wholesale.
package wikiparse

import (
	"regexp"
	"strings"
)

// Block is one CURATOR-marked region recovered from page content.
type Block struct {
	// ID is the marker's block= attribute.
	ID string

	// Zone is the marker's zone= attribute: "machine" or "editorial".
	// Empty if the marker omitted it (older renders, hand-edits).
	Zone string

	// Provenance is the marker's provenance= attribute (typically a
	// sha256 hash). Empty if the marker omitted it.
	Provenance string

	// Body is the raw text between BEGIN and END (excluding the
	// markers themselves). Reconciler treats this as opaque text for
	// preservation purposes.
	Body string

	// FollowingText is any text between this block's END marker and
	// the next BEGIN (or EOF). Preserved verbatim so reassembly is
	// lossless.
	FollowingText string
}

// ParsedDoc is the full parse result.
type ParsedDoc struct {
	Prologue string
	Blocks   []Block
	Epilogue string
}

// BlockByID returns the parsed block with the given ID, if any.
func (d *ParsedDoc) BlockByID(id string) (Block, bool) {
	for _, b := range d.Blocks {
		if b.ID == id {
			return b, true
		}
	}
	return Block{}, false
}

// beginRE matches `<!-- CURATOR:BEGIN block=ID [zone=Z] [provenance=HASH] -->`.
// Attributes are tolerant: extra whitespace, missing provenance, and
// missing zone are all accepted. Attributes after `block=` may
// appear in any order so older renders parse cleanly.
var beginRE = regexp.MustCompile(`<!--\s*CURATOR:BEGIN\s+block=([^\s]+)((?:\s+\w+=[^\s]+)*)\s*-->`)

// attrRE pulls individual key=value attributes from the captured
// attribute suffix of a BEGIN marker.
var attrRE = regexp.MustCompile(`(\w+)=([^\s]+)`)

// endREFor returns a regexp for the matching END marker of a given
// block ID. We compile per-block (small N; not worth optimising).
func endREFor(id string) *regexp.Regexp {
	// Escape user-supplied id for safety.
	return regexp.MustCompile(`<!--\s*CURATOR:END\s+block=` + regexp.QuoteMeta(id) + `\s*-->`)
}

// Parse extracts CURATOR blocks from content.
func Parse(content []byte) ParsedDoc {
	doc := ParsedDoc{}
	if len(content) == 0 {
		return doc
	}

	text := string(content)
	pos := 0

	for {
		// Find the next BEGIN at-or-after pos.
		loc := beginRE.FindStringSubmatchIndex(text[pos:])
		if loc == nil {
			// No more markers — everything from pos to EOF is either
			// epilogue (after a block) or prologue (no blocks at all).
			if len(doc.Blocks) == 0 {
				doc.Prologue = text[pos:]
			} else {
				doc.Epilogue = text[pos:]
				// Trailing text after the last END is epilogue.
				last := len(doc.Blocks) - 1
				doc.Blocks[last].FollowingText = ""
			}
			return doc
		}

		// Adjust indices for the slice-into-text.
		beginStart := pos + loc[0]
		beginEnd := pos + loc[1]
		matches := beginRE.FindStringSubmatch(text[pos:])
		id := matches[1]
		attrs := ""
		if len(matches) >= 3 {
			attrs = matches[2]
		}
		var prov, zone string
		for _, a := range attrRE.FindAllStringSubmatch(attrs, -1) {
			switch a[1] {
			case "provenance":
				prov = a[2]
			case "zone":
				zone = a[2]
			}
		}

		// Text between previous block end (or start) and this BEGIN.
		interstitial := text[pos:beginStart]
		if len(doc.Blocks) == 0 {
			doc.Prologue += interstitial
		} else {
			doc.Blocks[len(doc.Blocks)-1].FollowingText += interstitial
		}

		// Find the matching END.
		endRE := endREFor(id)
		endLoc := endRE.FindStringIndex(text[beginEnd:])
		if endLoc == nil {
			// Unclosed BEGIN — treat the marker itself as plain text
			// and stop block recognition. The remainder becomes
			// prologue/epilogue accordingly.
			rest := text[beginStart:]
			if len(doc.Blocks) == 0 {
				doc.Prologue += rest
			} else {
				doc.Blocks[len(doc.Blocks)-1].FollowingText += rest
			}
			return doc
		}
		endStart := beginEnd + endLoc[0]
		endEnd := beginEnd + endLoc[1]

		body := text[beginEnd:endStart]
		// Strip a single leading and trailing newline for ergonomic
		// equality — markers themselves usually have their own
		// surrounding newlines.
		body = strings.TrimPrefix(body, "\n")
		body = strings.TrimSuffix(body, "\n")

		doc.Blocks = append(doc.Blocks, Block{
			ID:         id,
			Zone:       zone,
			Provenance: prov,
			Body:       body,
		})

		pos = endEnd
	}
}
