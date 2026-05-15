// Package memory implements wiki.Target as an in-process store.
//
// Used for:
//   - Tests that need a real Target without spinning up a wiki
//     container (cheaper than testcontainers-go for L1+L2 tests).
//   - Local dry-run mode where the operator wants the curator to
//     "render and push" but to a discardable target.
//
// Concurrency: safe for concurrent reads + writes via a sync.Mutex.
package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
)

// Target is the in-memory wiki target.
type Target struct {
	mu       sync.Mutex
	identity string
	pages    map[string]*pageState // keyed by normalised title
}

// pageState holds a page's revision history newest-first, with
// content for each revision aligned to the same index.
type pageState struct {
	revisions []wiki.Revision
	contents  []string
}

// New constructs a Target authenticated as the given bot identity.
// Every UpsertPage call records a revision marked IsBot=true under
// this identity.
func New(identity string) *Target {
	return &Target{
		identity: identity,
		pages:    make(map[string]*pageState),
	}
}

// Whoami returns the configured bot identity.
func (t *Target) Whoami(_ context.Context) (string, error) {
	return t.identity, nil
}

// GetPage returns the current state of the named page, or nil if it
// doesn't exist.
func (t *Target) GetPage(_ context.Context, title string) (*wiki.Page, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p, ok := t.pages[normTitle(title)]
	if !ok || len(p.revisions) == 0 {
		return nil, nil
	}
	return &wiki.Page{
		Title:          title,
		Content:        p.contents[0],
		LatestRevision: p.revisions[0],
	}, nil
}

// UpsertPage creates or updates the page with content + summary.
// The new revision is recorded as a bot edit under the Target's identity.
func (t *Target) UpsertPage(_ context.Context, title, content, summary string) (wiki.Revision, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p := t.getOrCreate(title)
	rev := wiki.Revision{
		ID:        newRevID(),
		User:      t.identity,
		Timestamp: time.Now().UTC(),
		Comment:   summary,
		IsBot:     true,
	}
	p.prepend(rev, content)
	return rev, nil
}

// History returns the page's revisions newest-first. sinceRevID is
// honoured: if non-empty, only revisions newer than that ID are
// returned (excluding the boundary).
func (t *Target) History(_ context.Context, title, sinceRevID string) ([]wiki.Revision, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p, ok := t.pages[normTitle(title)]
	if !ok {
		return nil, nil
	}
	if sinceRevID == "" {
		out := make([]wiki.Revision, len(p.revisions))
		copy(out, p.revisions)
		return out, nil
	}
	var out []wiki.Revision
	for _, r := range p.revisions {
		if r.ID == sinceRevID {
			break
		}
		out = append(out, r)
	}
	return out, nil
}

// HumanEditsSinceBot scans the page's history for non-bot revisions
// recorded after the revision identified by lastBotRevID. Returns
// the first such revision (newest-first traversal, so this is the
// most recent human edit). Returns nil if no human edits are found.
func (t *Target) HumanEditsSinceBot(_ context.Context, title, lastBotRevID string) (*wiki.HumanEdit, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p, ok := t.pages[normTitle(title)]
	if !ok {
		return nil, nil
	}
	for _, r := range p.revisions {
		if r.ID == lastBotRevID {
			break
		}
		if !r.IsBot {
			return &wiki.HumanEdit{Revision: r}, nil
		}
	}
	return nil, nil
}

// simulateHumanEdit inserts a non-bot revision. Test helper — not
// part of the Target interface; used by the unit tests to set up
// human-edit reconciliation scenarios.
func (t *Target) simulateHumanEdit(title, user, content, summary string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p := t.getOrCreate(title)
	p.prepend(wiki.Revision{
		ID:        newRevID(),
		User:      user,
		Timestamp: time.Now().UTC(),
		Comment:   summary,
		IsBot:     false,
	}, content)
}

func (t *Target) getOrCreate(title string) *pageState {
	key := normTitle(title)
	p, ok := t.pages[key]
	if !ok {
		p = &pageState{}
		t.pages[key] = p
	}
	return p
}

func (p *pageState) prepend(r wiki.Revision, content string) {
	p.revisions = append([]wiki.Revision{r}, p.revisions...)
	p.contents = append([]string{content}, p.contents...)
}

// normTitle normalises a page title to MediaWiki conventions: spaces
// and underscores are interchangeable.
func normTitle(t string) string {
	return strings.ReplaceAll(t, " ", "_")
}

// newRevID returns a short hex string. Cryptographic strength is
// not needed; we just need uniqueness across a test run.
func newRevID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
