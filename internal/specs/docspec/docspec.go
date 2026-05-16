// Package docspec parses and validates the doc-spec language —
// Spec-Driven Development for documentation (see
// docs/doc-spec-format.md).
//
// A doc-spec describes a topic CLUSTER: a parent page plus
// cross-linked child pages. Each page declares its kind, audience,
// an intent (the acceptance contract the rendered page must
// satisfy), and ordered sections; each section declares its own
// intent + provenance sources. This package only parses and
// validates — it does not render or orchestrate (later slices).
package docspec

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Known enumerations. Kept here so validation is the single source
// of truth for the language.
var (
	knownKinds     = map[string]bool{"architecture": true, "runbook": true, "reference": true, "integration": true, "index": true}
	knownAudiences = map[string]bool{"": true, "human-operator": true, "newcomer": true, "llm-reference": true}
	knownRenders   = map[string]bool{"": true, "table": true, "child-index": true}
	knownSchemes   = map[string]bool{"kb": true, "git": true, "cmd": true, "ssh": true, "file": true}
)

// DocSpec is one topic cluster.
type DocSpec struct {
	Topic    string
	Parent   DocPage
	Children []DocPage
}

// DocPage is a single rendered wiki page within the cluster.
type DocPage struct {
	Page       string
	Kind       string
	Audience   string
	Intent     string
	Sections   []DocSection
	Sources    []Source // page-level sources (simple pages without sections)
	Related    []string
	Categories []string
}

// DocSection is one ordered section of a page.
type DocSection struct {
	Title   string
	Intent  string
	Render  string // "" = prose narrative; table; child-index
	Sources []Source
}

// Source is a declared provenance for a section/page. Scheme is one
// of kb|git|cmd|ssh|file; Spec is the scheme-specific remainder.
// Only kb is resolved today; the rest are reserved for the
// tool-using curator (the reality-probe family).
type Source struct {
	Scheme string
	Spec   string
	Raw    string
}

// ---- YAML shapes (mapped to the public model by Parse) ----

type docYAML struct {
	Topic    string     `yaml:"topic"`
	Parent   pageYAML   `yaml:"parent"`
	Children []pageYAML `yaml:"children"`
}

type pageYAML struct {
	Page       string        `yaml:"page"`
	Kind       string        `yaml:"kind"`
	Audience   string        `yaml:"audience"`
	Intent     string        `yaml:"intent"`
	Sections   []sectionYAML `yaml:"sections"`
	Sources    []string      `yaml:"sources"`
	Related    []string      `yaml:"related"`
	Categories []string      `yaml:"categories"`
}

type sectionYAML struct {
	Title   string   `yaml:"title"`
	Intent  string   `yaml:"intent"`
	Render  string   `yaml:"render"`
	Sources []string `yaml:"sources"`
}

// Parse decodes + validates a doc-spec document.
func Parse(b []byte) (DocSpec, error) {
	var y docYAML
	if err := yaml.Unmarshal(b, &y); err != nil {
		return DocSpec{}, fmt.Errorf("docspec: yaml: %w", err)
	}

	d := DocSpec{Topic: strings.TrimSpace(y.Topic)}
	parent, err := toPage(y.Parent, "parent")
	if err != nil {
		return DocSpec{}, err
	}
	d.Parent = parent
	for i, c := range y.Children {
		cp, err := toPage(c, fmt.Sprintf("children[%d]", i))
		if err != nil {
			return DocSpec{}, err
		}
		d.Children = append(d.Children, cp)
	}

	if err := d.validate(); err != nil {
		return DocSpec{}, err
	}
	return d, nil
}

func toPage(p pageYAML, where string) (DocPage, error) {
	dp := DocPage{
		Page:       strings.TrimSpace(p.Page),
		Kind:       p.Kind,
		Audience:   p.Audience,
		Intent:     strings.TrimSpace(p.Intent),
		Related:    p.Related,
		Categories: p.Categories,
	}
	srcs, err := parseSources(p.Sources, where)
	if err != nil {
		return DocPage{}, err
	}
	dp.Sources = srcs
	for i, s := range p.Sections {
		ss, err := parseSources(s.Sources, fmt.Sprintf("%s.sections[%d]", where, i))
		if err != nil {
			return DocPage{}, err
		}
		dp.Sections = append(dp.Sections, DocSection{
			Title:   strings.TrimSpace(s.Title),
			Intent:  strings.TrimSpace(s.Intent),
			Render:  s.Render,
			Sources: ss,
		})
	}
	return dp, nil
}

func parseSources(raw []string, where string) ([]Source, error) {
	var out []Source
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		i := strings.IndexByte(r, ':')
		if i <= 0 {
			return nil, fmt.Errorf("docspec: %s: source %q missing scheme (want scheme:spec)", where, r)
		}
		scheme := r[:i]
		if !knownSchemes[scheme] {
			return nil, fmt.Errorf("docspec: %s: unknown source scheme %q (known: kb, git, cmd, ssh, file)", where, scheme)
		}
		out = append(out, Source{
			Scheme: scheme,
			Spec:   strings.TrimSpace(r[i+1:]),
			Raw:    r,
		})
	}
	return out, nil
}

func (d DocSpec) validate() error {
	if d.Topic == "" {
		return fmt.Errorf("docspec: topic: required")
	}
	seen := map[string]bool{}
	check := func(p DocPage, where string) error {
		if p.Page == "" {
			return fmt.Errorf("docspec: %s.page: required", where)
		}
		if seen[p.Page] {
			return fmt.Errorf("docspec: duplicate page %q", p.Page)
		}
		seen[p.Page] = true
		if !knownKinds[p.Kind] {
			return fmt.Errorf("docspec: %s.kind: %q invalid (architecture|runbook|reference|integration|index)", where, p.Kind)
		}
		if !knownAudiences[p.Audience] {
			return fmt.Errorf("docspec: %s.audience: %q invalid (human-operator|newcomer|llm-reference)", where, p.Audience)
		}
		for i, s := range p.Sections {
			if s.Title == "" {
				return fmt.Errorf("docspec: %s.sections[%d]: title required", where, i)
			}
			if !knownRenders[s.Render] {
				return fmt.Errorf("docspec: %s.sections[%d] (%q): render %q invalid (table|child-index)", where, i, s.Title, s.Render)
			}
		}
		return nil
	}
	if err := check(d.Parent, "parent"); err != nil {
		return err
	}
	for i := range d.Children {
		if err := check(d.Children[i], fmt.Sprintf("children[%d]", i)); err != nil {
			return err
		}
	}
	return nil
}
