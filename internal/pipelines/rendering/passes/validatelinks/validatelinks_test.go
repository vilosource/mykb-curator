package validatelinks

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
)

var _ passes.Pass = (*ValidateLinks)(nil)

func TestName(t *testing.T) {
	if got := New(nil).Name(); got != "validate-links" {
		t.Errorf("Name = %q, want %q", got, "validate-links")
	}
}

func TestApply_NoLinks_OK(t *testing.T) {
	doc := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{ir.ProseBlock{Text: "plain prose"}},
	}}}
	out, err := New(nil).Apply(context.Background(), doc)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(out.Sections) != 1 {
		t.Errorf("document should pass through unchanged")
	}
}

func TestApply_KnownInternalLink_OK(t *testing.T) {
	known := map[string]bool{"Existing_Page": true}
	doc := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{ir.ProseBlock{Text: "See [[Existing_Page]]."}},
	}}}
	_, err := New(known).Apply(context.Background(), doc)
	if err != nil {
		t.Errorf("known page should not error: %v", err)
	}
}

func TestApply_UnknownInternalLink_Errors(t *testing.T) {
	known := map[string]bool{"Existing_Page": true}
	doc := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{ir.ProseBlock{Text: "See [[Ghost_Page]]."}},
	}}}
	_, err := New(known).Apply(context.Background(), doc)
	if err == nil {
		t.Fatalf("expected error for unknown link, got nil")
	}
	if !errors.Is(err, ErrBrokenLinks) {
		t.Errorf("error should wrap ErrBrokenLinks: %v", err)
	}
	if !strings.Contains(err.Error(), "Ghost_Page") {
		t.Errorf("error should name the broken link: %v", err)
	}
}

func TestApply_PipedLink_ChecksTarget(t *testing.T) {
	// [[Target|display]] — pipe-aliased link; target is what we
	// validate.
	known := map[string]bool{"Real_Target": true}
	docOK := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{ir.ProseBlock{Text: "See [[Real_Target|the docs]]."}},
	}}}
	if _, err := New(known).Apply(context.Background(), docOK); err != nil {
		t.Errorf("piped link to known page should pass: %v", err)
	}

	docBad := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{ir.ProseBlock{Text: "See [[Ghost|the docs]]."}},
	}}}
	if _, err := New(known).Apply(context.Background(), docBad); err == nil {
		t.Errorf("piped link to unknown target should fail")
	}
}

func TestApply_MultipleBrokenLinks_AllNamed(t *testing.T) {
	doc := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{
			ir.ProseBlock{Text: "See [[A]] and [[B]] and [[C]]."},
		},
	}}}
	_, err := New(map[string]bool{}).Apply(context.Background(), doc)
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	for _, name := range []string{"A", "B", "C"} {
		if !strings.Contains(msg, name) {
			t.Errorf("error should list every broken link; missing %q in: %v", name, msg)
		}
	}
}

func TestApply_NilKnownMap_AllLinksTreatedAsBroken(t *testing.T) {
	// Nil map means "we have no knowledge of which pages exist" — be
	// conservative and treat every link as broken so the operator
	// must wire the spec to declare its targets.
	doc := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{ir.ProseBlock{Text: "See [[Page]]."}},
	}}}
	_, err := New(nil).Apply(context.Background(), doc)
	if err == nil {
		t.Errorf("nil known map should treat link as broken")
	}
}

func TestApply_ExternalURL_NotChecked(t *testing.T) {
	// External URLs (http://, https://) are not the validator's job.
	// LinkRotCheck in the maintenance pipeline handles those.
	doc := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{
			ir.ProseBlock{Text: "See https://example.com and [[Known]]."},
		},
	}}}
	_, err := New(map[string]bool{"Known": true}).Apply(context.Background(), doc)
	if err != nil {
		t.Errorf("external URL should not affect link validation: %v", err)
	}
}
