// Package mediawiki implements wiki.Target for MediaWiki, on top of
// go-mwclient.
//
// Authentication: BotPasswords (Special:BotPasswords) — the
// recommended path for bot accounts. Username + password are
// supplied via Config; the curator's composition root resolves the
// password from an env var named in the wiki config file (secrets
// never live in config plaintext).
//
// What the adapter does not yet do (parked for follow-up slices):
//   - History pagination beyond ~50 revisions
//   - File uploads (RenderDiagrams pass)
//   - Atomic-edit via basetimestamp (race with concurrent human edit)
//   - Throttle / retry on maxlag
package mediawiki

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	mwclient "cgt.name/pkg/go-mwclient"
	"cgt.name/pkg/go-mwclient/params"

	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
)

// Config is the constructor input for a MediaWiki target.
type Config struct {
	// APIURL is the wiki's api.php endpoint (e.g.
	// https://wiki.example.com/api.php).
	APIURL string

	// BotUser is the bot account username (typically "User:Mykb-Curator").
	BotUser string

	// BotPass is the bot password secret. Resolved from env by the
	// composition root before reaching this struct.
	BotPass string

	// UserAgent is the HTTP User-Agent header. Defaults to a
	// curator-identifying string if empty.
	UserAgent string
}

// Target implements wiki.Target on top of go-mwclient.
type Target struct {
	cfg    Config
	client *mwclient.Client
}

// New constructs a Target. Validates URL on construction; defers
// network I/O (login) until the first Pull or Upsert.
func New(cfg Config) (*Target, error) {
	if cfg.UserAgent == "" {
		cfg.UserAgent = "mykb-curator/0.0"
	}
	c, err := mwclient.New(cfg.APIURL, cfg.UserAgent)
	if err != nil {
		return nil, fmt.Errorf("mediawiki: new client: %w", err)
	}
	c.Assert = mwclient.AssertBot
	return &Target{cfg: cfg, client: c}, nil
}

// ensureAuth logs in if we haven't yet. Idempotent: go-mwclient
// keeps the session cookie alive after the first login.
func (t *Target) ensureAuth(_ context.Context) error {
	if t.cfg.BotUser == "" {
		// Allow unauthenticated read-only use.
		return nil
	}
	return t.client.Login(t.cfg.BotUser, t.cfg.BotPass)
}

// Whoami returns the configured bot identity. Cheaper + more
// predictable than asking the wiki — the bot identity is set at
// construction time and doesn't change at runtime.
func (t *Target) Whoami(_ context.Context) (string, error) {
	if t.cfg.BotUser == "" {
		return "", errors.New("mediawiki: no bot user configured")
	}
	return t.cfg.BotUser, nil
}

// GetPage fetches the current state of a page. Returns nil if the
// page does not exist (not an error).
func (t *Target) GetPage(ctx context.Context, title string) (*wiki.Page, error) {
	if err := t.ensureAuth(ctx); err != nil {
		return nil, fmt.Errorf("mediawiki: auth: %w", err)
	}
	resp, err := t.client.Get(params.Values{
		"action":  "query",
		"prop":    "revisions",
		"titles":  title,
		"rvprop":  "ids|user|timestamp|comment|content|flags",
		"rvslots": "main",
	})
	if err != nil {
		return nil, fmt.Errorf("mediawiki: get page: %w", err)
	}
	pagesObj, err := resp.GetObject("query", "pages")
	if err != nil {
		return nil, nil
	}
	for _, raw := range pagesObj.Map() {
		page, err := raw.Object()
		if err != nil {
			continue
		}
		if _, missing := page.Map()["missing"]; missing {
			return nil, nil
		}
		revs, err := page.GetObjectArray("revisions")
		if err != nil || len(revs) == 0 {
			return nil, nil
		}
		rev := revs[0]
		latest := decodeRevision(rev)
		content := decodeContent(rev)
		return &wiki.Page{
			Title:          title,
			Content:        content,
			LatestRevision: latest,
		}, nil
	}
	return nil, nil
}

// UpsertPage creates or updates a page. bot=true is set so the
// revision shows the bot flag in history.
func (t *Target) UpsertPage(ctx context.Context, title, content, summary string) (wiki.Revision, error) {
	if err := t.ensureAuth(ctx); err != nil {
		return wiki.Revision{}, fmt.Errorf("mediawiki: auth: %w", err)
	}
	err := t.client.Edit(params.Values{
		"title":   title,
		"text":    content,
		"summary": summary,
		"bot":     "1",
	})
	if err != nil {
		return wiki.Revision{}, fmt.Errorf("mediawiki: edit %q: %w", title, err)
	}
	// Read back the latest revision to populate the Revision struct.
	page, err := t.GetPage(ctx, title)
	if err != nil || page == nil {
		return wiki.Revision{}, fmt.Errorf("mediawiki: edit succeeded but get-back failed")
	}
	return page.LatestRevision, nil
}

// History fetches revision metadata, newest first. sinceRevID, when
// non-empty, truncates the result at that revision (exclusive).
func (t *Target) History(ctx context.Context, title, sinceRevID string) ([]wiki.Revision, error) {
	if err := t.ensureAuth(ctx); err != nil {
		return nil, fmt.Errorf("mediawiki: auth: %w", err)
	}
	resp, err := t.client.Get(params.Values{
		"action":  "query",
		"prop":    "revisions",
		"titles":  title,
		"rvprop":  "ids|user|timestamp|comment|flags",
		"rvlimit": "50",
	})
	if err != nil {
		return nil, fmt.Errorf("mediawiki: history: %w", err)
	}
	pagesObj, err := resp.GetObject("query", "pages")
	if err != nil {
		return nil, nil
	}
	var out []wiki.Revision
	for _, raw := range pagesObj.Map() {
		page, err := raw.Object()
		if err != nil {
			continue
		}
		revs, _ := page.GetObjectArray("revisions")
		for _, r := range revs {
			rev := decodeRevision(r)
			if sinceRevID != "" && rev.ID == sinceRevID {
				return out, nil
			}
			out = append(out, rev)
		}
	}
	return out, nil
}

// HumanEditsSinceBot walks the page's history newest-first and
// returns the first non-bot revision encountered. Returns nil if
// the most recent revisions are all by the bot.
func (t *Target) HumanEditsSinceBot(ctx context.Context, title, lastBotRevID string) (*wiki.HumanEdit, error) {
	hist, err := t.History(ctx, title, "")
	if err != nil {
		return nil, err
	}
	for _, r := range hist {
		if r.ID == lastBotRevID {
			return nil, nil
		}
		if !r.IsBot {
			return &wiki.HumanEdit{Revision: r}, nil
		}
	}
	return nil, nil
}

// decodeRevision pulls the common fields off a revisions[] object.
func decodeRevision(rev any) wiki.Revision {
	type jsoner interface {
		GetString(...string) (string, error)
		GetInt64(...string) (int64, error)
		GetBoolean(...string) (bool, error)
	}
	r, ok := rev.(jsoner)
	if !ok {
		return wiki.Revision{}
	}
	user, _ := r.GetString("user")
	comment, _ := r.GetString("comment")
	tsStr, _ := r.GetString("timestamp")
	ts, _ := time.Parse(time.RFC3339, tsStr)
	revID, _ := r.GetInt64("revid")
	// MW marks bot edits with a "bot" key (presence-only in the
	// flags list). go-mwclient surfaces it as a boolean field on
	// the revision object.
	isBot, _ := r.GetBoolean("bot")
	return wiki.Revision{
		ID:        strconv.FormatInt(revID, 10),
		User:      user,
		Timestamp: ts,
		Comment:   comment,
		IsBot:     isBot,
	}
}

// decodeContent pulls the rendered text from a revision. MW 1.32+
// returns content via slots; older API responses use a direct "*"
// field. We handle both.
func decodeContent(rev any) string {
	type stringer interface {
		GetString(...string) (string, error)
	}
	r, ok := rev.(stringer)
	if !ok {
		return ""
	}
	if c, err := r.GetString("slots", "main", "*"); err == nil {
		return c
	}
	if c, err := r.GetString("*"); err == nil {
		return c
	}
	return ""
}
