// Package curatorapi is the localhost HTTP adapter the spec-chat
// Node agent-service calls (design D1: no shell-scrape across the
// language boundary). It is a pure transport over INJECTED engine
// interfaces — zero domain logic, zero composition. The `serve`
// cobra subcommand wires it from the existing compose* helpers.
//
// Endpoints (the Node<->Go contract; also the slice-3 client shape):
//
//	POST /v1/kb/area          {area}            -> {area:{...entries}}
//	POST /v1/doc-spec/get     {id}              -> {id,yaml,pages}
//	POST /v1/doc-spec/put     {id,edits}        -> {yaml,diff}        (gated in Node)
//	POST /v1/kb/propose-entry {area,type,text,source,why} -> {entry_id}
//	POST /v1/preview          {id,edits?}       -> {pages,verdicts,cost}
//
// This file lands the read-only core (area + doc-spec get); the
// mutating + preview endpoints arrive in the same slice.
package curatorapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/vilosource/mykb-curator/internal/adapters/docspecs"
	kbpkg "github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
	"github.com/vilosource/mykb-curator/internal/specs/docspec/docedit"
)

// KBWriter persists a proposed brain entry. The real impl shells the
// sanctioned stable `kb add <type>` CLI (design D6: never direct
// JSONL, never kb-develop). Source is mandatory; entry lands in the
// incoming/unverified zone — the agent may never self-verify.
type KBWriter interface {
	AddEntry(ctx context.Context, area, typ, text, source, why string) (entryID string, err error)
}

// Previewer renders a candidate spec and Judges it (design D3:
// composite, render+judge in one shot). Kept an interface so the
// adapter holds zero render/judge composition; `serve` wires the
// real cluster.Render + judge.Review + markdown backend.
type Previewer interface {
	Preview(ctx context.Context, candidateYAML []byte) (PreviewResult, error)
}

// PreviewResult is the composite render+judge outcome.
type PreviewResult struct {
	Pages            []PreviewPage `json:"pages"`
	AllPass          bool          `json:"all_pass"`
	Verdicts         []Verdict     `json:"verdicts"`
	UngroundedClaims []string      `json:"ungrounded_claims"`
	CostUSD          float64       `json:"cost_usd"`
}

// PreviewPage is one rendered page's markdown.
type PreviewPage struct {
	Page     string `json:"page"`
	Markdown string `json:"markdown"`
}

// Verdict is one section's Judge result.
type Verdict struct {
	Section string `json:"section"`
	Pass    bool   `json:"pass"`
	Reason  string `json:"reason"`
}

// KBReader is the read side of the brain (kb.Source satisfies it).
type KBReader interface {
	Pull(ctx context.Context) (kbpkg.Snapshot, error)
}

// DocSpecStore discovers parsed *.doc.yaml clusters (docspecs.Store).
type DocSpecStore interface {
	Pull(ctx context.Context) ([]docspecs.File, error)
}

// Server is the injected-dependency HTTP handler.
type Server struct {
	kb       KBReader
	specs    DocSpecStore
	specsDir string // where *.doc.yaml live, for put/preview
	kbw      KBWriter
	prev     Previewer
	mux      *http.ServeMux
}

// New builds the handler. specsDir is the on-disk doc-spec directory
// (same path the DocSpecStore reads) so mutating endpoints can splice
// files in place. kbw/prev may be nil if those endpoints are unwired.
func New(kb KBReader, specs DocSpecStore, specsDir string, kbw KBWriter, prev Previewer) *Server {
	s := &Server{kb: kb, specs: specs, specsDir: specsDir, kbw: kbw, prev: prev, mux: http.NewServeMux()}
	s.mux.HandleFunc("/v1/kb/area", s.handleKBArea)
	s.mux.HandleFunc("/v1/doc-spec/get", s.handleDocSpecGet)
	s.mux.HandleFunc("/v1/doc-spec/put", s.handleDocSpecPut)
	s.mux.HandleFunc("/v1/kb/propose-entry", s.handleProposeEntry)
	s.mux.HandleFunc("/v1/preview", s.handlePreview)
	return s
}

// editDTO is one mutation op (the Node<->Go contract for put/preview).
//
//	op=set_page_intent      ref,           value
//	op=set_section_intent   ref, section,  value
//	op=set_section_sources  ref, section,  sources[]
//	op=add_section_source   ref, section,  source
type editDTO struct {
	Op      string   `json:"op"`
	Ref     string   `json:"ref"` // "parent" | child page title
	Section string   `json:"section,omitempty"`
	Value   string   `json:"value,omitempty"`
	Source  string   `json:"source,omitempty"`
	Sources []string `json:"sources,omitempty"`
}

func pageRef(ref string) docedit.PageRef {
	if ref == "parent" {
		return docedit.ParentPage()
	}
	return docedit.ChildPage(ref)
}

// applyEdits decodes + applies the edit ops to a docedit Document.
func applyEdits(doc *docedit.Document, edits []editDTO) error {
	for i, e := range edits {
		var err error
		switch e.Op {
		case "set_page_intent":
			err = doc.SetPageIntent(pageRef(e.Ref), e.Value)
		case "set_section_intent":
			err = doc.SetSectionIntent(pageRef(e.Ref), e.Section, e.Value)
		case "set_section_sources":
			err = doc.SetSectionSources(pageRef(e.Ref), e.Section, e.Sources)
		case "add_section_source":
			err = doc.AddSectionSource(pageRef(e.Ref), e.Section, e.Source)
		default:
			return &editError{i, "unknown op " + e.Op}
		}
		if err != nil {
			return &editError{i, err.Error()}
		}
	}
	return nil
}

type editError struct {
	idx int
	msg string
}

func (e *editError) Error() string {
	return "edit[" + itoa(e.idx) + "]: " + e.msg
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// ---- wire DTOs (the Node<->Go JSON contract) ----------------------

type errorResp struct {
	Error string `json:"error"`
}

type kbAreaReq struct {
	Area string `json:"area"`
}

type entryDTO struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Text   string `json:"text"`
	Zone   string `json:"zone"`
	Source string `json:"source,omitempty"`
	Status string `json:"status,omitempty"`
	Why    string `json:"why,omitempty"`
	URL    string `json:"url,omitempty"`
}

type kbAreaResp struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Summary string     `json:"summary"`
	Entries []entryDTO `json:"entries"`
}

type docSpecGetReq struct {
	ID string `json:"id"`
}

type sectionDTO struct {
	Title   string   `json:"title"`
	Intent  string   `json:"intent"`
	Render  string   `json:"render,omitempty"`
	Sources []string `json:"sources"`
}

type pageDTO struct {
	Ref      string       `json:"ref"` // "parent" | child page title
	Page     string       `json:"page"`
	Kind     string       `json:"kind"`
	Intent   string       `json:"intent"`
	Sections []sectionDTO `json:"sections"`
}

type docSpecGetResp struct {
	ID    string    `json:"id"`
	Topic string    `json:"topic"`
	YAML  string    `json:"yaml"`
	Pages []pageDTO `json:"pages"`
}

// ---- helpers ------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errorResp{Error: msg})
}

// decodePOST enforces POST + a JSON body, the uniform contract.
func decodePOST(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "POST required")
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		fail(w, http.StatusBadRequest, "bad json body: "+err.Error())
		return false
	}
	return true
}

// ---- handlers -----------------------------------------------------

func (s *Server) handleKBArea(w http.ResponseWriter, r *http.Request) {
	var req kbAreaReq
	if !decodePOST(w, r, &req) {
		return
	}
	if req.Area == "" {
		fail(w, http.StatusBadRequest, "area required")
		return
	}
	snap, err := s.kb.Pull(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, "kb pull: "+err.Error())
		return
	}
	a := snap.Area(req.Area)
	if a == nil {
		fail(w, http.StatusNotFound, "area not found: "+req.Area)
		return
	}
	resp := kbAreaResp{ID: a.ID, Name: a.Name, Summary: a.Summary}
	for _, e := range a.Entries {
		resp.Entries = append(resp.Entries, entryDTO{
			ID: e.ID, Type: e.Type, Text: e.Text, Zone: e.Zone,
			Source: e.Provenance.Source, Status: e.Provenance.Status,
			Why: e.Why, URL: e.URL,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func pageToDTO(ref string, p docspec.DocPage) pageDTO {
	d := pageDTO{Ref: ref, Page: p.Page, Kind: p.Kind, Intent: p.Intent}
	for _, sec := range p.Sections {
		sd := sectionDTO{Title: sec.Title, Intent: sec.Intent, Render: sec.Render}
		for _, src := range sec.Sources {
			sd.Sources = append(sd.Sources, src.Raw)
		}
		d.Sections = append(d.Sections, sd)
	}
	return d
}

func (s *Server) handleDocSpecGet(w http.ResponseWriter, r *http.Request) {
	var req docSpecGetReq
	if !decodePOST(w, r, &req) {
		return
	}
	if req.ID == "" {
		fail(w, http.StatusBadRequest, "id required")
		return
	}
	files, err := s.specs.Pull(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, "docspec pull: "+err.Error())
		return
	}
	var found *docspecs.File
	for i := range files {
		if files[i].ID == req.ID {
			found = &files[i]
			break
		}
	}
	if found == nil {
		fail(w, http.StatusNotFound, "doc-spec not found: "+req.ID)
		return
	}
	raw, err := os.ReadFile(filepath.Join(s.specsDir, req.ID))
	if err != nil {
		fail(w, http.StatusInternalServerError, "read spec file: "+err.Error())
		return
	}
	resp := docSpecGetResp{ID: found.ID, Topic: found.Spec.Topic, YAML: string(raw)}
	resp.Pages = append(resp.Pages, pageToDTO("parent", found.Spec.Parent))
	for _, c := range found.Spec.Children {
		resp.Pages = append(resp.Pages, pageToDTO(c.Page, c))
	}
	writeJSON(w, http.StatusOK, resp)
}

type docSpecPutReq struct {
	ID    string    `json:"id"`
	Edits []editDTO `json:"edits"`
}

type docSpecPutResp struct {
	ID   string `json:"id"`
	YAML string `json:"yaml"`
	Diff string `json:"diff"`
}

// safeSpecPath joins id under specsDir and rejects traversal.
func (s *Server) safeSpecPath(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	p := filepath.Join(s.specsDir, id)
	rel, err := filepath.Rel(s.specsDir, p)
	if err != nil || rel == ".." || filepath.IsAbs(rel) ||
		len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return "", false
	}
	return p, true
}

// handleDocSpecPut applies edits and writes the file in place. The
// propose->ACK HITL gate (design D2/D6) lives in the Node layer; by
// the time this is called the human has approved, so it writes.
func (s *Server) handleDocSpecPut(w http.ResponseWriter, r *http.Request) {
	var req docSpecPutReq
	if !decodePOST(w, r, &req) {
		return
	}
	path, ok := s.safeSpecPath(req.ID)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid id")
		return
	}
	orig, err := os.ReadFile(path)
	if err != nil {
		fail(w, http.StatusNotFound, "read spec: "+err.Error())
		return
	}
	doc, err := docedit.Parse(orig)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "parse spec: "+err.Error())
		return
	}
	if err := applyEdits(doc, req.Edits); err != nil {
		fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	out, err := doc.Bytes()
	if err != nil {
		fail(w, http.StatusInternalServerError, "emit: "+err.Error())
		return
	}
	if _, err := docspec.Parse(out); err != nil {
		fail(w, http.StatusUnprocessableEntity, "edited spec invalid: "+err.Error())
		return
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fail(w, http.StatusInternalServerError, "write spec: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, docSpecPutResp{
		ID: req.ID, YAML: string(out), Diff: unifiedish(string(orig), string(out)),
	})
}

type proposeEntryReq struct {
	Area   string `json:"area"`
	Type   string `json:"type"`
	Text   string `json:"text"`
	Source string `json:"source"`
	Why    string `json:"why,omitempty"`
}

type proposeEntryResp struct {
	EntryID string `json:"entry_id"`
	Area    string `json:"area"`
	Zone    string `json:"zone"`
}

var knownEntryTypes = map[string]bool{"fact": true, "decision": true, "gotcha": true, "pattern": true}

func (s *Server) handleProposeEntry(w http.ResponseWriter, r *http.Request) {
	var req proposeEntryReq
	if !decodePOST(w, r, &req) {
		return
	}
	if s.kbw == nil {
		fail(w, http.StatusNotImplemented, "kb writer not wired")
		return
	}
	switch {
	case req.Area == "":
		fail(w, http.StatusBadRequest, "area required")
		return
	case !knownEntryTypes[req.Type]:
		fail(w, http.StatusBadRequest, "type must be fact|decision|gotcha|pattern")
		return
	case req.Text == "":
		fail(w, http.StatusBadRequest, "text required")
		return
	case req.Source == "":
		// design D6: provenance is MANDATORY (verification-first).
		fail(w, http.StatusBadRequest, "source required (provenance is mandatory)")
		return
	}
	id, err := s.kbw.AddEntry(r.Context(), req.Area, req.Type, req.Text, req.Source, req.Why)
	if err != nil {
		fail(w, http.StatusBadGateway, "kb add: "+err.Error())
		return
	}
	// Zone is reported as "incoming" because the KBWriter contract
	// quarantines every proposed entry there — enforced by ShellKBWriter
	// passing `--zone incoming` and asserted on the real argv in
	// shellkbwriter_test (so this is no longer a hardcoded claim that
	// can drift from what is actually written — mykb-curator#2).
	writeJSON(w, http.StatusOK, proposeEntryResp{EntryID: id, Area: req.Area, Zone: "incoming"})
}

type previewReq struct {
	ID    string    `json:"id"`
	Edits []editDTO `json:"edits,omitempty"`
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req previewReq
	if !decodePOST(w, r, &req) {
		return
	}
	if s.prev == nil {
		fail(w, http.StatusNotImplemented, "previewer not wired")
		return
	}
	path, ok := s.safeSpecPath(req.ID)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid id")
		return
	}
	orig, err := os.ReadFile(path)
	if err != nil {
		fail(w, http.StatusNotFound, "read spec: "+err.Error())
		return
	}
	candidate := orig
	if len(req.Edits) > 0 {
		doc, derr := docedit.Parse(orig)
		if derr != nil {
			fail(w, http.StatusUnprocessableEntity, "parse spec: "+derr.Error())
			return
		}
		if aerr := applyEdits(doc, req.Edits); aerr != nil {
			fail(w, http.StatusUnprocessableEntity, aerr.Error())
			return
		}
		if candidate, err = doc.Bytes(); err != nil {
			fail(w, http.StatusInternalServerError, "emit: "+err.Error())
			return
		}
	}
	res, err := s.prev.Preview(r.Context(), candidate)
	if err != nil {
		fail(w, http.StatusBadGateway, "preview: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// unifiedish is a minimal, dependency-free line diff: "- " removed,
// "+ " added, by longest-common-subsequence. Enough for the agent to
// show the human what a put/widen changed before ACK.
func unifiedish(a, b string) string {
	al := splitLines(a)
	bl := splitLines(b)
	n, m := len(al), len(bl)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var sb []byte
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case al[i] == bl[j]:
			sb = append(sb, "  "...)
			sb = append(sb, al[i]...)
			sb = append(sb, '\n')
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			sb = append(sb, "- "...)
			sb = append(sb, al[i]...)
			sb = append(sb, '\n')
			i++
		default:
			sb = append(sb, "+ "...)
			sb = append(sb, bl[j]...)
			sb = append(sb, '\n')
			j++
		}
	}
	for ; i < n; i++ {
		sb = append(sb, "- "...)
		sb = append(sb, al[i]...)
		sb = append(sb, '\n')
	}
	for ; j < m; j++ {
		sb = append(sb, "+ "...)
		sb = append(sb, bl[j]...)
		sb = append(sb, '\n')
	}
	return string(sb)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
