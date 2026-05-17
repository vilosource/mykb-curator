//go:build contract

// Pyramid level 3 — the curator-api WIRE contract. These raw-JSON
// request/response shapes are exactly what the slice-3 Node
// agent-service depends on; this test is the reviewed, stable
// contract between the two processes (design D1). Changing a field
// here is a deliberate cross-process API change.
package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/curatorapi"
	"github.com/vilosource/mykb-curator/internal/adapters/docspecs"
	kbpkg "github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

type cKB struct{}

func (cKB) Pull(context.Context) (kbpkg.Snapshot, error) {
	return kbpkg.Snapshot{Areas: []kbpkg.Area{{
		ID: "vault", Name: "Vault", Summary: "HashiCorp Vault",
		Entries: []kbpkg.Entry{{
			ID: "f1", Type: "fact", Text: "daily Raft snapshot to GRS",
			Zone: "established", Provenance: kbpkg.EntryProvenance{Status: "verified", Source: "runbook"},
		}},
	}}}, nil
}

type cSpecs struct{ f []docspecs.File }

func (c cSpecs) Pull(context.Context) ([]docspecs.File, error) { return c.f, nil }

type cKBW struct{}

func (cKBW) AddEntry(context.Context, string, string, string, string, string) (string, error) {
	return "newid42", nil
}

type cPrev struct{}

func (cPrev) Preview(context.Context, []byte) (curatorapi.PreviewResult, error) {
	return curatorapi.PreviewResult{
		AllPass:          false,
		Pages:            []curatorapi.PreviewPage{{Page: "Vault Architecture", Markdown: "# x"}},
		Verdicts:         []curatorapi.Verdict{{Section: "Deployment & Operations", Pass: false, Reason: "ungrounded"}},
		UngroundedClaims: []string{"daily Raft snapshot to GRS"},
		CostUSD:          0.13,
	}, nil
}

func keys(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("response not a JSON object: %v\n%s", err, b)
	}
	return m
}

func hasAll(t *testing.T, m map[string]any, want ...string) {
	t.Helper()
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Fatalf("contract: missing field %q in %v", k, m)
		}
	}
}

func call(t *testing.T, ts *httptest.Server, path, body string) ([]byte, int) {
	t.Helper()
	resp, err := http.Post(ts.URL+path, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, resp.StatusCode
}

func TestCuratorAPI_WireContract(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "specs", "docspec", "docedit", "testdata", "vault.doc.yaml"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vault.doc.yaml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	spec, err := docspec.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	api := curatorapi.New(cKB{}, cSpecs{f: []docspecs.File{{ID: "vault.doc.yaml", Spec: spec}}},
		dir, cKBW{}, cPrev{})
	ts := httptest.NewServer(api)
	defer ts.Close()

	// read_kb_area
	b, code := call(t, ts, "/v1/kb/area", `{"area":"vault"}`)
	if code != 200 {
		t.Fatalf("kb/area %d: %s", code, b)
	}
	m := keys(t, b)
	hasAll(t, m, "id", "name", "summary", "entries")
	ent := m["entries"].([]any)[0].(map[string]any)
	hasAll(t, ent, "id", "type", "text", "zone", "source", "status")

	// doc-spec/get
	b, code = call(t, ts, "/v1/doc-spec/get", `{"id":"vault.doc.yaml"}`)
	if code != 200 {
		t.Fatalf("doc-spec/get %d: %s", code, b)
	}
	m = keys(t, b)
	hasAll(t, m, "id", "topic", "yaml", "pages")
	pg := m["pages"].([]any)[0].(map[string]any)
	hasAll(t, pg, "ref", "page", "kind", "intent", "sections")
	sec := pg["sections"].([]any)[0].(map[string]any)
	hasAll(t, sec, "title", "intent", "sources")

	// doc-spec/put (widen-sources edit op)
	b, code = call(t, ts, "/v1/doc-spec/put",
		`{"id":"vault.doc.yaml","edits":[{"op":"add_section_source","ref":"parent","section":"Deployment & Operations","source":"kb:area=disaster-recovery"}]}`)
	if code != 200 {
		t.Fatalf("doc-spec/put %d: %s", code, b)
	}
	hasAll(t, keys(t, b), "id", "yaml", "diff")

	// kb/propose-entry
	b, code = call(t, ts, "/v1/kb/propose-entry",
		`{"area":"vault","type":"fact","text":"daily Raft snapshot to GRS","source":"runbook"}`)
	if code != 200 {
		t.Fatalf("propose-entry %d: %s", code, b)
	}
	hasAll(t, keys(t, b), "entry_id", "area", "zone")

	// kb/propose-entry — provenance contract: no source => 400
	if _, code = call(t, ts, "/v1/kb/propose-entry",
		`{"area":"vault","type":"fact","text":"x"}`); code != 400 {
		t.Fatalf("provenance contract: want 400 without source, got %d", code)
	}

	// preview
	b, code = call(t, ts, "/v1/preview", `{"id":"vault.doc.yaml"}`)
	if code != 200 {
		t.Fatalf("preview %d: %s", code, b)
	}
	m = keys(t, b)
	hasAll(t, m, "pages", "all_pass", "verdicts", "ungrounded_claims", "cost_usd")
	v := m["verdicts"].([]any)[0].(map[string]any)
	hasAll(t, v, "section", "pass", "reason")
}
