package curatorapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/docspecs"
	kbpkg "github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

type fakeKB struct {
	snap kbpkg.Snapshot
	err  error
}

func (f fakeKB) Pull(context.Context) (kbpkg.Snapshot, error) { return f.snap, f.err }

type fakeSpecs struct {
	files []docspecs.File
	err   error
}

func (f fakeSpecs) Pull(context.Context) ([]docspecs.File, error) { return f.files, f.err }

type fakeKBW struct {
	gotArea, gotType, gotText, gotSource, gotWhy string
	id                                           string
	err                                          error
}

func (f *fakeKBW) AddEntry(_ context.Context, area, typ, text, source, why string) (string, error) {
	f.gotArea, f.gotType, f.gotText, f.gotSource, f.gotWhy = area, typ, text, source, why
	return f.id, f.err
}

type fakePrev struct {
	got []byte
	res PreviewResult
	err error
}

func (f *fakePrev) Preview(_ context.Context, candidate []byte) (PreviewResult, error) {
	f.got = candidate
	return f.res, f.err
}

func newTestSpec(t *testing.T) (dir string, spec docspec.DocSpec, raw []byte) {
	t.Helper()
	dir = t.TempDir()
	var err error
	raw, err = os.ReadFile(filepath.Join("..", "..", "specs", "docspec", "docedit", "testdata", "vault.doc.yaml"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "vault.doc.yaml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if spec, err = docspec.Parse(raw); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return
}

func post(t *testing.T, h http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestKBArea_OK(t *testing.T) {
	kb := fakeKB{snap: kbpkg.Snapshot{Areas: []kbpkg.Area{{
		ID: "vault", Name: "Vault", Summary: "HashiCorp Vault",
		Entries: []kbpkg.Entry{{
			ID: "e1", Type: "fact", Text: "Raft snapshot to GRS daily",
			Zone: "established", Provenance: kbpkg.EntryProvenance{Status: "verified", Source: "runbook"},
		}},
	}}}}
	s := New(kb, fakeSpecs{}, "", nil, nil)
	w := post(t, s, "/v1/kb/area", kbAreaReq{Area: "vault"})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body)
	}
	var got kbAreaResp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "vault" || len(got.Entries) != 1 || got.Entries[0].Source != "runbook" {
		t.Fatalf("unexpected resp: %+v", got)
	}
}

func TestKBArea_NotFound(t *testing.T) {
	s := New(fakeKB{snap: kbpkg.Snapshot{}}, fakeSpecs{}, "", nil, nil)
	w := post(t, s, "/v1/kb/area", kbAreaReq{Area: "nope"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestKBArea_BadInput(t *testing.T) {
	s := New(fakeKB{}, fakeSpecs{}, "", nil, nil)
	if w := post(t, s, "/v1/kb/area", kbAreaReq{}); w.Code != http.StatusBadRequest {
		t.Fatalf("empty area: want 400, got %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/kb/area", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: want 405, got %d", w.Code)
	}
}

func TestDocSpecGet_OK(t *testing.T) {
	dir, spec, raw := newTestSpec(t)
	s := New(fakeKB{}, fakeSpecs{files: []docspecs.File{{ID: "vault.doc.yaml", Spec: spec}}}, dir, nil, nil)
	w := post(t, s, "/v1/doc-spec/get", docSpecGetReq{ID: "vault.doc.yaml"})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body)
	}
	var got docSpecGetResp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Topic != "Vault" {
		t.Fatalf("topic=%q", got.Topic)
	}
	if string(raw) != got.YAML {
		t.Fatalf("yaml not returned verbatim")
	}
	if got.Pages[0].Ref != "parent" || got.Pages[0].Page != "Vault Architecture" {
		t.Fatalf("parent page wrong: %+v", got.Pages[0])
	}
	// "Deployment & Operations" must carry its two flow sources.
	var depSec *sectionDTO
	for i := range got.Pages[0].Sections {
		if got.Pages[0].Sections[i].Title == "Deployment & Operations" {
			depSec = &got.Pages[0].Sections[i]
		}
	}
	if depSec == nil || len(depSec.Sources) != 2 {
		t.Fatalf("Deployment & Operations sources wrong: %+v", depSec)
	}
}

func TestDocSpecGet_NotFound(t *testing.T) {
	s := New(fakeKB{}, fakeSpecs{files: nil}, t.TempDir(), nil, nil)
	if w := post(t, s, "/v1/doc-spec/get", docSpecGetReq{ID: "missing.doc.yaml"}); w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

// put_doc_spec over slice-1 docedit: the widen-sources path that
// closes a GRS-type gap. File written in place, comments preserved,
// diff returned, result still valid docspec.
func TestDocSpecPut_WidenSources(t *testing.T) {
	dir, _, raw := newTestSpec(t)
	s := New(fakeKB{}, fakeSpecs{}, dir, nil, nil)
	w := post(t, s, "/v1/doc-spec/put", docSpecPutReq{
		ID: "vault.doc.yaml",
		Edits: []editDTO{{
			Op: "add_section_source", Ref: "parent",
			Section: "Deployment & Operations", Source: "kb:area=disaster-recovery",
		}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body)
	}
	var got docSpecPutResp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "vault.doc.yaml"))
	if string(onDisk) != got.YAML {
		t.Fatal("returned yaml != file on disk")
	}
	if len(onDisk) <= len(raw) || !bytes.Contains(onDisk, []byte("kb:area=disaster-recovery")) {
		t.Fatalf("widen not applied:\n%s", onDisk)
	}
	if !bytes.Contains(onDisk, []byte("# The 1:1 machine-oriented dump")) {
		t.Fatal("comment lost through put")
	}
	if got.Diff == "" || !bytes.Contains([]byte(got.Diff), []byte("+ ")) {
		t.Fatalf("diff missing/empty: %q", got.Diff)
	}
	spec, err := docspec.Parse(onDisk)
	if err != nil {
		t.Fatalf("written spec invalid: %v", err)
	}
	_ = spec
}

func TestDocSpecPut_BadEditRejected(t *testing.T) {
	dir, _, _ := newTestSpec(t)
	s := New(fakeKB{}, fakeSpecs{}, dir, nil, nil)
	w := post(t, s, "/v1/doc-spec/put", docSpecPutReq{
		ID:    "vault.doc.yaml",
		Edits: []editDTO{{Op: "set_section_intent", Ref: "parent", Section: "No Such Section", Value: "x"}},
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d body=%s", w.Code, w.Body)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "vault.doc.yaml"))
	if !bytes.Contains(onDisk, []byte("topic: Vault")) {
		t.Fatal("file mutated despite rejected edit")
	}
}

func TestDocSpecPut_PathTraversalRejected(t *testing.T) {
	dir, _, _ := newTestSpec(t)
	s := New(fakeKB{}, fakeSpecs{}, dir, nil, nil)
	if w := post(t, s, "/v1/doc-spec/put", docSpecPutReq{ID: "../../etc/x"}); w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for traversal, got %d", w.Code)
	}
}

func TestProposeEntry_ProvenanceMandatory(t *testing.T) {
	kbw := &fakeKBW{id: "newid7"}
	s := New(fakeKB{}, fakeSpecs{}, "", kbw, nil)

	// Missing source -> 400, writer never called (design D6).
	w := post(t, s, "/v1/kb/propose-entry", proposeEntryReq{
		Area: "vault", Type: "fact", Text: "Raft snapshot to GRS daily",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing source: want 400, got %d", w.Code)
	}
	if kbw.gotArea != "" {
		t.Fatal("kb writer called despite missing provenance")
	}

	// Bad type -> 400.
	if w := post(t, s, "/v1/kb/propose-entry", proposeEntryReq{
		Area: "vault", Type: "lemma", Text: "x", Source: "s",
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("bad type: want 400, got %d", w.Code)
	}

	// Valid -> writer invoked with exact args, zone=incoming.
	w = post(t, s, "/v1/kb/propose-entry", proposeEntryReq{
		Area: "vault", Type: "fact", Text: "daily Raft snapshot -> GRS",
		Source: "hashicorp-vault runbook", Why: "",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("valid: code=%d body=%s", w.Code, w.Body)
	}
	var got proposeEntryResp
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.EntryID != "newid7" || got.Zone != "incoming" {
		t.Fatalf("resp wrong: %+v", got)
	}
	if kbw.gotArea != "vault" || kbw.gotType != "fact" || kbw.gotSource != "hashicorp-vault runbook" {
		t.Fatalf("writer got wrong args: %+v", kbw)
	}
}

func TestProposeEntry_Unwired(t *testing.T) {
	s := New(fakeKB{}, fakeSpecs{}, "", nil, nil)
	if w := post(t, s, "/v1/kb/propose-entry", proposeEntryReq{
		Area: "v", Type: "fact", Text: "t", Source: "s",
	}); w.Code != http.StatusNotImplemented {
		t.Fatalf("want 501 when kb writer unwired, got %d", w.Code)
	}
}

// preview_spec applies edits to a candidate (NOT written to disk) and
// hands it to the Previewer; the response is the composite result.
func TestPreview_CandidateNotWritten(t *testing.T) {
	dir, _, raw := newTestSpec(t)
	prev := &fakePrev{res: PreviewResult{
		AllPass:  true,
		Pages:    []PreviewPage{{Page: "Vault Architecture", Markdown: "# ok"}},
		Verdicts: []Verdict{{Section: "Deployment & Operations", Pass: true, Reason: "grounded"}},
		CostUSD:  0.42,
	}}
	s := New(fakeKB{}, fakeSpecs{}, dir, nil, prev)
	w := post(t, s, "/v1/preview", previewReq{
		ID: "vault.doc.yaml",
		Edits: []editDTO{{
			Op: "add_section_source", Ref: "parent",
			Section: "Deployment & Operations", Source: "kb:area=disaster-recovery",
		}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body)
	}
	if !bytes.Contains(prev.got, []byte("kb:area=disaster-recovery")) {
		t.Fatal("previewer did not receive the edited candidate")
	}
	onDisk, _ := os.ReadFile(filepath.Join(dir, "vault.doc.yaml"))
	if !bytes.Equal(onDisk, raw) {
		t.Fatal("preview must NOT write the file (candidate only)")
	}
	var got PreviewResult
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if !got.AllPass || got.CostUSD != 0.42 || len(got.Verdicts) != 1 {
		t.Fatalf("preview result not surfaced: %+v", got)
	}
}
