// Package docedit applies surgical, fidelity-preserving edits to a
// hand-authored .doc.yaml file.
//
// Design decision D2(a) (docs/spec-chat-agent-DESIGN.md): the
// spec-chat agent must edit a .doc.yaml in place without clobbering
// the human's comments, key order, folded-scalar intents, or
// flow-vs-block sequence styling. A struct -> yaml.Marshal round-trip
// destroys all of that, and yaml.v3 cannot even re-emit its own node
// tree byte-identically (it re-wraps folded scalars and injects blank
// lines). So docedit uses the yaml.v3 node tree ONLY to locate the
// line span of the node being changed, then splices the replacement
// into the original source text. Every untouched byte round-trips
// verbatim by construction.
//
// Only the mutations v1 needs are exposed (D5 scope): set a page or
// section intent, set or widen a section's sources.
package docedit

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Document is a parsed .doc.yaml retaining full formatting fidelity.
// src is the authoritative substrate; node is re-derived from it
// after every edit so line numbers stay valid.
type Document struct {
	src  []byte
	node *yaml.Node // DocumentNode, re-parsed from src
}

// PageRef selects the parent page or a child page by its title.
type PageRef struct {
	parent bool
	page   string
}

// ParentPage refers to the cluster's parent page.
func ParentPage() PageRef { return PageRef{parent: true} }

// ChildPage refers to the child page with the given `page:` title.
func ChildPage(title string) PageRef { return PageRef{page: title} }

func (r PageRef) String() string {
	if r.parent {
		return "parent"
	}
	return fmt.Sprintf("child %q", r.page)
}

// Parse decodes src and validates it is a single-document mapping.
func Parse(src []byte) (*Document, error) {
	d := &Document{src: append([]byte(nil), src...)}
	if err := d.reparse(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *Document) reparse() error {
	var doc yaml.Node
	if err := yaml.Unmarshal(d.src, &doc); err != nil {
		return fmt.Errorf("docedit: parse: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 ||
		doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("docedit: not a single-document mapping")
	}
	d.node = &doc
	return nil
}

// Bytes returns the current source. With no edits applied it is the
// original bytes verbatim.
func (d *Document) Bytes() ([]byte, error) {
	return append([]byte(nil), d.src...), nil
}

func (d *Document) root() *yaml.Node { return d.node.Content[0] }

// mapEntry returns the key and value nodes for key in a MappingNode.
func mapEntry(m *yaml.Node, key string) (k, v *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

func mapVal(m *yaml.Node, key string) *yaml.Node {
	_, v := mapEntry(m, key)
	return v
}

func (d *Document) pageNode(ref PageRef) (*yaml.Node, error) {
	root := d.root()
	if ref.parent {
		p := mapVal(root, "parent")
		if p == nil {
			return nil, fmt.Errorf("docedit: %s: no parent page", ref)
		}
		return p, nil
	}
	children := mapVal(root, "children")
	if children == nil || children.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("docedit: %s: no children", ref)
	}
	for _, c := range children.Content {
		if pg := mapVal(c, "page"); pg != nil && strings.TrimSpace(pg.Value) == ref.page {
			return c, nil
		}
	}
	return nil, fmt.Errorf("docedit: %s: page not found", ref)
}

func (d *Document) sectionNode(ref PageRef, title string) (*yaml.Node, error) {
	page, err := d.pageNode(ref)
	if err != nil {
		return nil, err
	}
	secs := mapVal(page, "sections")
	if secs == nil || secs.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("docedit: %s: no sections", ref)
	}
	for _, s := range secs.Content {
		if t := mapVal(s, "title"); t != nil && strings.TrimSpace(t.Value) == title {
			return s, nil
		}
	}
	return nil, fmt.Errorf("docedit: %s: section %q not found", ref, title)
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// valueLineSpan returns [start,end) 0-based line indices covering the
// value of `key` inside mapping m. The value runs from its first line
// to the line before the next construct at indentation <= the key's
// (a sibling key, a parent dedent, or a sequence dash). This captures
// multi-line folded/literal scalars and their continuation lines.
func valueLineSpan(lines []string, m *yaml.Node, key string) (start, end int, ok bool) {
	kNode, vNode := mapEntry(m, key)
	if kNode == nil || vNode == nil {
		return 0, 0, false
	}
	start = vNode.Line - 1
	keyIndent := kNode.Column - 1
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		ln := lines[i]
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if indentOf(ln) <= keyIndent {
			end = i
			break
		}
	}
	// Trim trailing blank lines back out of the span (they belong to
	// spacing/comments that follow, not to this value).
	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return start, end, true
}

func (d *Document) splice(start, end int, repl []string) error {
	lines := strings.Split(string(d.src), "\n")
	if start < 0 || end > len(lines) || start > end {
		return fmt.Errorf("docedit: bad splice span [%d,%d) of %d", start, end, len(lines))
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, repl...)
	out = append(out, lines[end:]...)
	d.src = []byte(strings.Join(out, "\n"))
	return d.reparse()
}

// setIntent rewrites the `intent:` of mapping m (a page or section).
// The new value is emitted as a folded block scalar when it is long
// (the authored convention for multi-line intents) and as a plain
// scalar when short, matching how humans write these files.
func (d *Document) setIntent(m *yaml.Node, parentDesc, intent string) error {
	lines := strings.Split(string(d.src), "\n")
	kNode, _ := mapEntry(m, "intent")
	if kNode == nil {
		return fmt.Errorf("docedit: %s: no intent to set", parentDesc)
	}
	start, end, ok := valueLineSpan(lines, m, "intent")
	if !ok {
		return fmt.Errorf("docedit: %s: cannot locate intent span", parentDesc)
	}
	keyIndent := strings.Repeat(" ", kNode.Column-1)
	intent = strings.TrimSpace(intent)

	var repl []string
	if len(intent) <= 70 && !strings.ContainsAny(intent, "\n:#") {
		repl = []string{fmt.Sprintf("%sintent: %s", keyIndent, intent)}
	} else {
		repl = []string{fmt.Sprintf("%sintent: >", keyIndent)}
		contIndent := keyIndent + "  "
		for _, w := range wrapWords(intent, 66) {
			repl = append(repl, contIndent+w)
		}
	}
	return d.splice(start, end, repl)
}

// SetPageIntent rewrites a page's `intent:`.
func (d *Document) SetPageIntent(ref PageRef, intent string) error {
	page, err := d.pageNode(ref)
	if err != nil {
		return err
	}
	return d.setIntent(page, ref.String(), intent)
}

// SetSectionIntent rewrites one section's `intent:`.
func (d *Document) SetSectionIntent(ref PageRef, sectionTitle, intent string) error {
	sec, err := d.sectionNode(ref, sectionTitle)
	if err != nil {
		return err
	}
	return d.setIntent(sec, fmt.Sprintf("%s section %q", ref, sectionTitle), intent)
}

// AddSectionSource appends one source to a section's `sources:`,
// preserving the list's existing flow-vs-block style. This is the
// widen-sources remedy (e.g. add kb:area=disaster-recovery to close a
// brain-content gap). Idempotent.
func (d *Document) AddSectionSource(ref PageRef, sectionTitle, source string) error {
	sec, err := d.sectionNode(ref, sectionTitle)
	if err != nil {
		return err
	}
	seq := mapVal(sec, "sources")
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return fmt.Errorf("docedit: %s section %q: no sources list to widen",
			ref, sectionTitle)
	}
	for _, e := range seq.Content {
		if e.Value == source {
			return nil // idempotent
		}
	}
	lines := strings.Split(string(d.src), "\n")

	if seq.Style == yaml.FlowStyle {
		li := seq.Line - 1
		ln := lines[li]
		idx := strings.LastIndexByte(ln, ']')
		if idx < 0 {
			return fmt.Errorf("docedit: malformed flow sources at line %d", seq.Line)
		}
		newLn := ln[:idx] + fmt.Sprintf(`, "%s"`, source) + ln[idx:]
		return d.splice(li, li+1, []string{newLn})
	}

	// Block style: insert a new "- " item after the last element,
	// matching the existing dash indentation.
	last := seq.Content[len(seq.Content)-1]
	dashIndent := strings.Repeat(" ", last.Column-1-2) // "- " is 2 cols
	if last.Column-1-2 < 0 {
		dashIndent = ""
	}
	at := last.Line // 0-based line AFTER the last element (last.Line is 1-based)
	newLn := fmt.Sprintf(`%s- "%s"`, dashIndent, source)
	return d.splice(at, at, []string{newLn})
}

// SetSectionSources replaces a section's `sources:` list wholesale,
// emitting block style (one quoted item per line).
func (d *Document) SetSectionSources(ref PageRef, sectionTitle string, sources []string) error {
	sec, err := d.sectionNode(ref, sectionTitle)
	if err != nil {
		return err
	}
	kNode, _ := mapEntry(sec, "sources")
	if kNode == nil {
		return fmt.Errorf("docedit: %s section %q: no sources key", ref, sectionTitle)
	}
	lines := strings.Split(string(d.src), "\n")
	_, end, ok := valueLineSpan(lines, sec, "sources")
	if !ok {
		return fmt.Errorf("docedit: %s section %q: cannot locate sources span", ref, sectionTitle)
	}
	// Replace from the "sources:" key line through the end of its
	// value (flow: same line; block: the indented "- " element lines).
	start := kNode.Line - 1
	keyIndent := strings.Repeat(" ", kNode.Column-1)
	repl := []string{keyIndent + "sources:"}
	for _, s := range sources {
		repl = append(repl, fmt.Sprintf(`%s  - "%s"`, keyIndent, s))
	}
	return d.splice(start, end, repl)
}

// wrapWords greedily wraps text to <=width-char lines on spaces.
func wrapWords(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	cur := words[0]
	for _, w := range words[1:] {
		if len(cur)+1+len(w) > width {
			out = append(out, cur)
			cur = w
			continue
		}
		cur += " " + w
	}
	return append(out, cur)
}
