package docedit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

func goldenBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "vault.doc.yaml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	return b
}

// The hardest-unknown property (D2 option a): parse a hand-authored
// .doc.yaml and re-emit it byte-for-byte — comments, key order,
// folded scalars, flow vs block sequence styles all preserved.
func TestRoundTripIdentity(t *testing.T) {
	src := goldenBytes(t)
	d, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := d.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(src, out) {
		t.Fatalf("round-trip not byte-identical\n--- want (%d) ---\n%s\n--- got (%d) ---\n%s",
			len(src), src, len(out), out)
	}
}

// widen-sources: the GRS-gap remedy. Add kb:area=disaster-recovery to
// the parent's "Deployment & Operations" section. Everything else —
// every comment, every other line — must be untouched.
func TestAddSectionSource_WidenSources(t *testing.T) {
	src := goldenBytes(t)
	d, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := d.AddSectionSource(ParentPage(), "Deployment & Operations", "kb:area=disaster-recovery"); err != nil {
		t.Fatalf("AddSectionSource: %v", err)
	}
	out, err := d.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	// 1. Still valid docspec, and the new source is present on that section.
	spec, err := docspec.Parse(out)
	if err != nil {
		t.Fatalf("edited spec no longer parses: %v", err)
	}
	var found bool
	for _, sec := range spec.Parent.Sections {
		if sec.Title == "Deployment & Operations" {
			for _, s := range sec.Sources {
				if s.Raw == "kb:area=disaster-recovery" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("new source not found on Deployment & Operations:\n%s", out)
	}

	// 2. The two distinctive hand-authored comments survive verbatim.
	for _, want := range []string{
		"# Vault topic cluster — a faithful projection of the hand-crafted",
		"# The 1:1 machine-oriented dump — deliberately a SEPARATE",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("lost authored comment %q after edit:\n%s", want, out)
		}
	}

	// 3. Diff confined to that one section's sources line: the
	//    section's sources is flow-style, so appending modifies that
	//    single line — every OTHER source line of the file is untouched.
	for _, keep := range []string{
		`sources: ["kb:area=vault"]`,
		`- "git:infrastructure/azure-optiscangroup/services/infra/infra-docker-stacks/hashicorp-vault"`,
		`sources: ["kb:area=disaster-recovery", "kb:area=vault"]`,
	} {
		if !strings.Contains(string(out), keep) {
			t.Fatalf("unrelated source line changed; lost %q:\n%s", keep, out)
		}
	}
}

// Block-style sources gets the clean surgical guarantee: appending is
// exactly one new line, nothing removed, no reformat.
func TestAddSectionSource_BlockStyle_Surgical(t *testing.T) {
	src := goldenBytes(t)
	d, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := d.AddSectionSource(ParentPage(), "Source Code & IaC", "kb:area=iac"); err != nil {
		t.Fatalf("AddSectionSource: %v", err)
	}
	out, err := d.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if _, err := docspec.Parse(out); err != nil {
		t.Fatalf("edited spec no longer parses: %v", err)
	}
	added, removed := lineDelta(string(src), string(out))
	if added != 1 || removed != 0 {
		t.Fatalf("block-style append not surgical: +%d -%d (want +1 -0)\n%s", added, removed, out)
	}
}

func TestSetSectionIntent(t *testing.T) {
	d, err := Parse(goldenBytes(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	const ni = "Rewritten intent for the routine-operations section."
	if err := d.SetSectionIntent(ChildPage("Vault Operations"), "Routine Operations", ni); err != nil {
		t.Fatalf("SetSectionIntent: %v", err)
	}
	out, err := d.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	spec, err := docspec.Parse(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	for _, c := range spec.Children {
		if c.Page != "Vault Operations" {
			continue
		}
		for _, s := range c.Sections {
			if s.Title == "Routine Operations" && s.Intent != ni {
				t.Fatalf("intent not set: %q", s.Intent)
			}
		}
	}
	if !strings.Contains(string(out), "# The 1:1 machine-oriented dump") {
		t.Fatalf("comment lost after intent edit")
	}
}

// The hardest setIntent case: replace the parent's multi-line folded
// `>` intent. Must stay valid, carry the new text, and leave both
// distinctive comments intact.
func TestSetPageIntent_FoldedLong(t *testing.T) {
	d, err := Parse(goldenBytes(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ni := "A reader with zero context grasps what Vault is and exactly how Optiscan runs, operates and recovers it across HA Raft, auto-unseal, Swarm ingress and DR."
	if err := d.SetPageIntent(ParentPage(), ni); err != nil {
		t.Fatalf("SetPageIntent: %v", err)
	}
	out, err := d.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	spec, err := docspec.Parse(out)
	if err != nil {
		t.Fatalf("re-parse after folded-intent edit: %v\n%s", err, out)
	}
	if !strings.Contains(spec.Parent.Intent, "grasps what Vault is") {
		t.Fatalf("parent intent not updated: %q", spec.Parent.Intent)
	}
	for _, want := range []string{
		"# Vault topic cluster — a faithful projection of the hand-crafted",
		"# The 1:1 machine-oriented dump — deliberately a SEPARATE",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("lost comment %q:\n%s", want, out)
		}
	}
	// The following section must be intact and correctly positioned.
	if !strings.Contains(string(out), "- title: System Architecture") {
		t.Fatalf("section after edited intent corrupted:\n%s", out)
	}
}

func TestSetSectionSources_Replace(t *testing.T) {
	d, err := Parse(goldenBytes(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := d.SetSectionSources(ParentPage(), "Disaster Recovery",
		[]string{"kb:area=disaster-recovery", "kb:area=vault", "kb:area=backup"}); err != nil {
		t.Fatalf("SetSectionSources: %v", err)
	}
	out, err := d.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	spec, err := docspec.Parse(out)
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, out)
	}
	for _, sec := range spec.Parent.Sections {
		if sec.Title != "Disaster Recovery" {
			continue
		}
		if len(sec.Sources) != 3 || sec.Sources[2].Raw != "kb:area=backup" {
			t.Fatalf("sources not replaced: %+v", sec.Sources)
		}
	}
	if !strings.Contains(string(out), "# The 1:1 machine-oriented dump") {
		t.Fatalf("comment lost after source replace")
	}
}

func TestAddSectionSource_Idempotent(t *testing.T) {
	d, err := Parse(goldenBytes(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	before, _ := d.Bytes()
	if err := d.AddSectionSource(ParentPage(), "System Architecture", "kb:area=vault"); err != nil {
		t.Fatalf("AddSectionSource: %v", err)
	}
	after, _ := d.Bytes()
	if !bytes.Equal(before, after) {
		t.Fatalf("adding an existing source was not a no-op\n%s", after)
	}
}

func TestUnknownRefsError(t *testing.T) {
	d, err := Parse(goldenBytes(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := d.AddSectionSource(ChildPage("No Such Page"), "x", "kb:area=y"); err == nil {
		t.Fatal("want error for unknown page ref")
	}
	if err := d.SetSectionIntent(ParentPage(), "No Such Section", "x"); err == nil {
		t.Fatal("want error for unknown section title")
	}
}

// lineDelta returns (#lines in b not in a, #lines in a not in b) by
// multiset difference — order-independent, good enough to assert "one
// line added, none removed" for a surgical edit.
func lineDelta(a, b string) (added, removed int) {
	ca := map[string]int{}
	for _, l := range strings.Split(a, "\n") {
		ca[l]++
	}
	cb := map[string]int{}
	for _, l := range strings.Split(b, "\n") {
		cb[l]++
	}
	for l, n := range cb {
		if n > ca[l] {
			added += n - ca[l]
		}
	}
	for l, n := range ca {
		if n > cb[l] {
			removed += n - cb[l]
		}
	}
	return
}
