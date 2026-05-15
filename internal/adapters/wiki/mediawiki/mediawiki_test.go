package mediawiki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
)

var _ wiki.Target = (*Target)(nil)

// fakeMW is a minimal MediaWiki API stub backed by httptest. It
// supports the request shapes go-mwclient sends: login (action=login),
// token fetch (action=query&meta=tokens), edit (action=edit), and
// page reads (action=query&prop=revisions). The behaviour is enough
// to exercise our adapter's wire-up — not enough to pretend to be
// a real wiki.
type fakeMW struct {
	mu       sync.Mutex
	loggedIn bool
	pages    map[string]string // title → latest content
	revCount int
}

func newFakeMW() *httptest.Server {
	mw := &fakeMW{pages: map[string]string{}}
	return httptest.NewServer(http.HandlerFunc(mw.serve))
}

func (m *fakeMW) serve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	action := firstNonEmpty(r.PostForm.Get("action"), r.URL.Query().Get("action"))
	w.Header().Set("Content-Type", "application/json")

	m.mu.Lock()
	defer m.mu.Unlock()

	switch action {
	case "login":
		// go-mwclient sends action=login WITH the lgtoken obtained
		// via meta=tokens (handled in the query branch). On Success
		// the wiki marks the session authenticated.
		m.loggedIn = true
		fmt.Fprintf(w, `{"login":{"result":"Success","lguserid":1,"lgusername":%q}}`, r.PostForm.Get("lgname"))
	case "query":
		meta := firstNonEmpty(r.PostForm.Get("meta"), r.URL.Query().Get("meta"))
		tokenType := firstNonEmpty(r.PostForm.Get("type"), r.URL.Query().Get("type"))
		if meta == "tokens" {
			switch tokenType {
			case "login":
				fmt.Fprintf(w, `{"query":{"tokens":{"logintoken":"fake-login-token+\\\\"}}}`)
			default:
				fmt.Fprintf(w, `{"query":{"tokens":{"csrftoken":"fake-csrf-token+\\\\"}}}`)
			}
			return
		}
		titles := firstNonEmpty(r.PostForm.Get("titles"), r.URL.Query().Get("titles"))
		prop := firstNonEmpty(r.PostForm.Get("prop"), r.URL.Query().Get("prop"))
		if strings.Contains(prop, "revisions") && titles != "" {
			content, ok := m.pages[titles]
			if !ok {
				fmt.Fprintf(w, `{"query":{"pages":{"-1":{"title":%q,"missing":""}}}}`, titles)
				return
			}
			// slots live inside each revision in MW 1.32+ format.
			fmt.Fprintf(w, `{"query":{"pages":{"1":{"title":%q,"revisions":[{"revid":%d,"user":"User:Bot","timestamp":"2026-01-01T00:00:00Z","comment":"test","slots":{"main":{"contentmodel":"wikitext","contentformat":"text/x-wiki","*":%q}}}]}}}}`, titles, m.revCount, content)
			return
		}
		fmt.Fprint(w, `{"query":{}}`)
	case "edit":
		title := firstNonEmpty(r.PostForm.Get("title"), r.URL.Query().Get("title"))
		text := firstNonEmpty(r.PostForm.Get("text"), r.URL.Query().Get("text"))
		if title == "" {
			http.Error(w, "title required", 400)
			return
		}
		m.pages[title] = text
		m.revCount++
		fmt.Fprintf(w, `{"edit":{"result":"Success","pageid":1,"title":%q,"newrevid":%d}}`, title, m.revCount)
	case "parse":
		page := firstNonEmpty(r.PostForm.Get("page"), r.URL.Query().Get("page"))
		content, ok := m.pages[page]
		if !ok {
			fmt.Fprintf(w, `{"error":{"code":"missingtitle","info":"page %s does not exist"}}`, page)
			return
		}
		jb, _ := json.Marshal(content)
		fv := firstNonEmpty(r.PostForm.Get("formatversion"), r.URL.Query().Get("formatversion"))
		if fv == "2" {
			// formatversion=2: wikitext is a plain string.
			fmt.Fprintf(w, `{"parse":{"title":%q,"pageid":1,"wikitext":%s}}`, page, jb)
		} else {
			fmt.Fprintf(w, `{"parse":{"title":%q,"pageid":1,"wikitext":{"*":%s}}}`, page, jb)
		}
	default:
		// Unknown action — return an api error shape go-mwclient understands.
		fmt.Fprintf(w, `{"error":{"code":"unknown-action","info":"unsupported action: %s"}}`, action)
	}
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func TestNew_FailsOnMalformedURL(t *testing.T) {
	if _, err := New(Config{APIURL: "://malformed", BotUser: "U", BotPass: "P"}); err == nil {
		t.Errorf("expected error for malformed URL, got nil")
	}
}

func TestUpsertPage_HappyPath(t *testing.T) {
	srv := newFakeMW()
	defer srv.Close()

	tgt, err := New(Config{APIURL: srv.URL, BotUser: "User:Bot", BotPass: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rev, err := tgt.UpsertPage(context.Background(), "TestPage", "hello world", "first edit")
	if err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}
	// v0.5: UpsertPage no longer issues an immediate readback, so the
	// revision ID is not populated. User + IsBot + Comment + summary
	// metadata are. The contract suite + scenario tests verify the
	// page actually landed by reading it back separately.
	if rev.User != "User:Bot" {
		t.Errorf("rev.User = %q, want %q", rev.User, "User:Bot")
	}
	if !rev.IsBot {
		t.Errorf("rev.IsBot = false, want true")
	}
	if rev.Comment != "first edit" {
		t.Errorf("rev.Comment = %q, want %q", rev.Comment, "first edit")
	}
}

func TestGetPage_AfterUpsert(t *testing.T) {
	srv := newFakeMW()
	defer srv.Close()

	tgt, err := New(Config{APIURL: srv.URL, BotUser: "User:Bot", BotPass: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := tgt.UpsertPage(context.Background(), "TestPage", "the content", "first"); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}
	page, err := tgt.GetPage(context.Background(), "TestPage")
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if page == nil {
		t.Fatalf("page is nil after upsert")
	}
	if !strings.Contains(page.Content, "the content") {
		t.Errorf("Content = %q, want to contain 'the content'", page.Content)
	}
}

func TestGetPage_Missing_ReturnsNil(t *testing.T) {
	srv := newFakeMW()
	defer srv.Close()

	tgt, err := New(Config{APIURL: srv.URL, BotUser: "U", BotPass: "P"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	page, err := tgt.GetPage(context.Background(), "NeverCreated")
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if page != nil {
		t.Errorf("expected nil for missing page, got %+v", page)
	}
}

func TestWhoami_ReturnsConfiguredBot(t *testing.T) {
	srv := newFakeMW()
	defer srv.Close()

	tgt, err := New(Config{APIURL: srv.URL, BotUser: "User:Mykb-Curator", BotPass: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := tgt.Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if got != "User:Mykb-Curator" {
		t.Errorf("Whoami = %q, want %q", got, "User:Mykb-Curator")
	}
}
