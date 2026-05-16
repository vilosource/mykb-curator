// Package parser reads spec files (markdown + YAML frontmatter) and
// produces specs.Spec values.
//
// File format:
//
//	---
//	wiki: <tenant>
//	page: <wiki page title>
//	kind: projection | editorial | hub | runbook
//	version: 1
//	include:
//	  areas: [a, b, c]
//	  workspaces: [d, e]
//	  exclude_zones: [incoming, archived]
//	fact_check:
//	  link_rot: every-run
//	  external_truth: quarterly
//	protected_blocks: [block-id]
//	---
//
//	<markdown body — the intent description>
//
// `wiki`, `page`, and `kind` are required. `kind` must be one of the
// known frontend kinds; unknown values are rejected so a typo never
// reaches the orchestrator.
package parser

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/vilosource/mykb-curator/internal/adapters/specs"
)

// knownKinds enumerates the frontend kinds this parser accepts.
// New frontends register here. The orchestrator dispatches on the
// same string.
var knownKinds = map[string]bool{
	"projection": true,
	"editorial":  true,
	"hub":        true,
	"runbook":    true,
}

// frontmatter mirrors the YAML schema. Mapped to specs.Spec by Parse.
type frontmatter struct {
	Wiki            string            `yaml:"wiki"`
	Page            string            `yaml:"page"`
	Kind            string            `yaml:"kind"`
	Version         int               `yaml:"version"`
	Include         includeYAML       `yaml:"include"`
	FactCheck       map[string]string `yaml:"fact_check"`
	ProtectedBlocks []string          `yaml:"protected_blocks"`
	Hub             *hubYAML          `yaml:"hub"`
}

type hubYAML struct {
	Sections []struct {
		Title string `yaml:"title"`
		Links []struct {
			Page  string `yaml:"page"`
			Label string `yaml:"label"`
			Desc  string `yaml:"desc"`
			Area  string `yaml:"area"`
		} `yaml:"links"`
	} `yaml:"sections"`
}

type includeYAML struct {
	Areas        []string     `yaml:"areas"`
	Workspaces   stringOrList `yaml:"workspaces"`
	ExcludeZones []string     `yaml:"exclude_zones"`
}

// stringOrList accepts either a YAML scalar string or a YAML sequence
// of strings and normalises both to []string.
//
// This lets specs write `workspaces: linked-to-areas` (sentinel) or
// `workspaces: [foo, bar]` (explicit list) interchangeably, per
// DESIGN.md §7.1.
type stringOrList []string

func (s *stringOrList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*s = []string{node.Value}
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		*s = list
		return nil
	default:
		return fmt.Errorf("expected string or list of strings, got yaml kind=%d", node.Kind)
	}
}

// fmDelim is the standard YAML-frontmatter delimiter.
var fmDelim = []byte("---")

// Parse reads one spec file. id is the spec's stable identifier
// (typically the file path inside the spec store, used for run
// reports and cache keys).
func Parse(id string, content []byte) (specs.Spec, error) {
	fm, body, err := splitFrontmatter(content)
	if err != nil {
		return specs.Spec{}, fmt.Errorf("spec %s: %w", id, err)
	}

	var f frontmatter
	if err := yaml.Unmarshal(fm, &f); err != nil {
		return specs.Spec{}, fmt.Errorf("spec %s: frontmatter yaml: %w", id, err)
	}

	if err := validateFrontmatter(&f); err != nil {
		return specs.Spec{}, fmt.Errorf("spec %s: %w", id, err)
	}

	return specs.Spec{
		ID:   id,
		Wiki: f.Wiki,
		Page: f.Page,
		Kind: f.Kind,
		Include: specs.IncludeFilter{
			Areas:        f.Include.Areas,
			Workspaces:   []string(f.Include.Workspaces),
			ExcludeZones: f.Include.ExcludeZones,
		},
		FactCheck: f.FactCheck,
		Hub:       toHubSpec(f.Hub),
		Body:      string(body),
		Hash:      hashContent(content),
	}, nil
}

// toHubSpec maps the YAML hub block onto the public spec model.
// Returns nil when absent so non-hub specs carry no hub structure.
func toHubSpec(h *hubYAML) *specs.HubSpec {
	if h == nil {
		return nil
	}
	hs := &specs.HubSpec{}
	for _, sec := range h.Sections {
		s := specs.HubSection{Title: sec.Title}
		for _, l := range sec.Links {
			s.Links = append(s.Links, specs.HubLink{
				Page: l.Page, Label: l.Label, Desc: l.Desc, Area: l.Area,
			})
		}
		hs.Sections = append(hs.Sections, s)
	}
	return hs
}

// splitFrontmatter separates a "---\n…\n---\nbody" pair into
// (frontmatter, body). Returns an error if the leading delimiter is
// not present or the closing delimiter is missing.
func splitFrontmatter(content []byte) (fm, body []byte, err error) {
	trimmed := bytes.TrimLeft(content, " \t\r\n")
	if !bytes.HasPrefix(trimmed, fmDelim) {
		return nil, nil, fmt.Errorf("missing frontmatter (expected leading '---')")
	}
	// advance past the first delimiter line
	rest := trimmed[len(fmDelim):]
	rest = trimLeadingNewline(rest)

	// find the closing delimiter on its own line
	closeIdx := bytes.Index(rest, append([]byte("\n"), fmDelim...))
	if closeIdx < 0 {
		return nil, nil, fmt.Errorf("missing closing frontmatter delimiter ('---')")
	}
	fm = rest[:closeIdx]
	body = rest[closeIdx+len(fmDelim)+1:] // +1 for the leading newline
	body = trimLeadingNewline(body)
	return fm, body, nil
}

func trimLeadingNewline(b []byte) []byte {
	if len(b) > 0 && b[0] == '\n' {
		return b[1:]
	}
	if len(b) > 1 && b[0] == '\r' && b[1] == '\n' {
		return b[2:]
	}
	return b
}

func validateFrontmatter(f *frontmatter) error {
	if f.Wiki == "" {
		return fmt.Errorf("wiki: required")
	}
	if f.Page == "" {
		return fmt.Errorf("page: required")
	}
	if f.Kind == "" {
		return fmt.Errorf("kind: required")
	}
	if !knownKinds[f.Kind] {
		return fmt.Errorf("kind: %q unknown (known: projection, editorial, hub, runbook)", f.Kind)
	}
	if f.Kind == "hub" {
		if f.Hub == nil || len(f.Hub.Sections) == 0 {
			return fmt.Errorf("hub: kind=hub requires a non-empty hub.sections")
		}
		for i, s := range f.Hub.Sections {
			if len(s.Links) == 0 {
				return fmt.Errorf("hub.sections[%d] (%q): has no links", i, s.Title)
			}
			for j, l := range s.Links {
				if l.Page == "" {
					return fmt.Errorf("hub.sections[%d].links[%d]: page is required", i, j)
				}
			}
		}
	}
	return nil
}

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
