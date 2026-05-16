package docspec_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

const vaultDoc = `
topic: Vault
parent:
  page: OptiscanGroup/Azure_Infrastructure/Vault_Architecture
  kind: architecture
  audience: human-operator
  intent: A human understands what Vault is and how it is built.
  sections:
    - title: System Architecture
      intent: Platform, storage, auto-unseal, network, identity.
      sources: ["kb:area=vault tag=ha,raft"]
    - title: Source Code & IaC
      render: table
      sources: ["git:infra-docker-stacks/hashicorp-vault"]
    - title: Operational Runbooks
      render: child-index
  related: [Docker_Swarm_Platform, Disaster_Recovery]
  categories: [Azure Infrastructure, Vault]
children:
  - page: OptiscanGroup/Azure_Infrastructure/Vault_Operations
    kind: runbook
    intent: Day-2 operations.
    sources: ["kb:area=vault tag=ops"]
  - page: OptiscanGroup/Azure_Infrastructure/Vault_Reference
    kind: reference
    audience: llm-reference
    sources: ["kb:area=vault"]
`

func TestParse_ValidVaultCluster(t *testing.T) {
	d, err := docspec.Parse([]byte(vaultDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if d.Topic != "Vault" {
		t.Errorf("Topic = %q", d.Topic)
	}
	if d.Parent.Page != "OptiscanGroup/Azure_Infrastructure/Vault_Architecture" ||
		d.Parent.Kind != "architecture" || d.Parent.Audience != "human-operator" {
		t.Errorf("parent wrong: %+v", d.Parent)
	}
	if len(d.Parent.Sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(d.Parent.Sections))
	}
	s0 := d.Parent.Sections[0]
	if s0.Title != "System Architecture" || s0.Render != "" {
		t.Errorf("s0 = %+v", s0)
	}
	if len(s0.Sources) != 1 || s0.Sources[0].Scheme != "kb" ||
		s0.Sources[0].Spec != "area=vault tag=ha,raft" {
		t.Errorf("s0 source parse wrong: %+v", s0.Sources)
	}
	if d.Parent.Sections[1].Render != "table" || d.Parent.Sections[2].Render != "child-index" {
		t.Errorf("render modes: %q %q", d.Parent.Sections[1].Render, d.Parent.Sections[2].Render)
	}
	if d.Parent.Sections[1].Sources[0].Scheme != "git" {
		t.Errorf("git source scheme: %+v", d.Parent.Sections[1].Sources)
	}
	if !reflect.DeepEqual(d.Parent.Related, []string{"Docker_Swarm_Platform", "Disaster_Recovery"}) {
		t.Errorf("related = %v", d.Parent.Related)
	}
	if len(d.Children) != 2 || d.Children[1].Audience != "llm-reference" || d.Children[1].Kind != "reference" {
		t.Errorf("children wrong: %+v", d.Children)
	}
}

func TestParse_Rejects(t *testing.T) {
	cases := map[string]string{
		"no topic":          "parent:\n  page: P\n  kind: architecture\n",
		"no parent.page":    "topic: T\nparent:\n  kind: architecture\n",
		"bad parent.kind":   "topic: T\nparent:\n  page: P\n  kind: bogus\n",
		"bad audience":      "topic: T\nparent:\n  page: P\n  kind: architecture\n  audience: robot\n",
		"bad render":        "topic: T\nparent:\n  page: P\n  kind: architecture\n  sections:\n    - title: S\n      render: carousel\n",
		"bad source scheme": "topic: T\nparent:\n  page: P\n  kind: architecture\n  sections:\n    - title: S\n      sources: [\"smtp:foo\"]\n",
		"section no title":  "topic: T\nparent:\n  page: P\n  kind: architecture\n  sections:\n    - intent: x\n",
		"child no kind":     "topic: T\nparent:\n  page: P\n  kind: architecture\nchildren:\n  - page: C\n",
		"duplicate page":    "topic: T\nparent:\n  page: SAME\n  kind: architecture\nchildren:\n  - page: SAME\n    kind: reference\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := docspec.Parse([]byte(doc)); err == nil {
				t.Errorf("expected parse/validation error for %q", name)
			}
		})
	}
}

func TestParse_SourceSchemes(t *testing.T) {
	for _, sch := range []string{"kb", "git", "cmd", "ssh", "file"} {
		doc := "topic: T\nparent:\n  page: P\n  kind: architecture\n  sections:\n    - title: S\n      sources: [\"" + sch + ":whatever here\"]\n"
		d, err := docspec.Parse([]byte(doc))
		if err != nil {
			t.Fatalf("scheme %q should be valid: %v", sch, err)
		}
		got := d.Parent.Sections[0].Sources[0]
		if got.Scheme != sch || got.Spec != "whatever here" || !strings.HasPrefix(got.Raw, sch+":") {
			t.Errorf("scheme %q parsed wrong: %+v", sch, got)
		}
	}
}
