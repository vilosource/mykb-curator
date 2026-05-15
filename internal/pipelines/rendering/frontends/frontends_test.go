package frontends

import (
	"context"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

type stubFrontend struct {
	name string
	kind string
}

func (s stubFrontend) Name() string { return s.name }
func (s stubFrontend) Kind() string { return s.kind }
func (s stubFrontend) Build(_ context.Context, _ specs.Spec, _ kb.Snapshot) (ir.Document, error) {
	return ir.Document{Frontmatter: ir.Frontmatter{Title: s.name}}, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	r.Register(stubFrontend{name: "proj", kind: "projection"})
	r.Register(stubFrontend{name: "edit", kind: "editorial"})

	f, err := r.For("projection")
	if err != nil {
		t.Fatalf("For(projection): %v", err)
	}
	if f.Name() != "proj" {
		t.Errorf("got %q, want proj", f.Name())
	}
}

func TestRegistry_For_UnknownKind_Errors(t *testing.T) {
	r := NewRegistry()
	_, err := r.For("missing")
	if err == nil {
		t.Errorf("expected error for unknown kind")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("err = %v, want mention of kind name", err)
	}
}

func TestRegistry_DuplicateRegistration_Panics(t *testing.T) {
	r := NewRegistry()
	r.Register(stubFrontend{name: "a", kind: "projection"})

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on duplicate registration, got none")
		}
	}()
	r.Register(stubFrontend{name: "b", kind: "projection"})
}

func TestRegistry_Kinds_ReturnsAllRegistered(t *testing.T) {
	r := NewRegistry()
	r.Register(stubFrontend{name: "a", kind: "projection"})
	r.Register(stubFrontend{name: "b", kind: "editorial"})
	r.Register(stubFrontend{name: "c", kind: "hub"})

	got := r.Kinds()
	if len(got) != 3 {
		t.Fatalf("len(Kinds) = %d, want 3", len(got))
	}
	set := map[string]bool{}
	for _, k := range got {
		set[k] = true
	}
	for _, want := range []string{"projection", "editorial", "hub"} {
		if !set[want] {
			t.Errorf("missing kind %q in Kinds() = %v", want, got)
		}
	}
}
