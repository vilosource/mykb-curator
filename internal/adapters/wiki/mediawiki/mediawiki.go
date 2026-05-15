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
	"encoding/json"
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

	// DisableBotAssert, when true, skips the MediaWiki `assert=bot`
	// guard the client otherwise applies to every request.
	// Production deployments leave this false — assertion is a
	// safety net that catches "your bot lost its group" silently.
	// Test wikis (and any setup where the bot identity doesn't have
	// a separate bot group) set it true.
	DisableBotAssert bool
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
	if !cfg.DisableBotAssert {
		c.Assert = mwclient.AssertBot
	}
	return &Target{cfg: cfg, client: c}, nil
}

// ensureAuth logs in if we haven't yet. Idempotent: go-mwclient
// keeps the session cookie alive after the first login.
//
// API warnings (e.g., deprecation notices about action=login) are
// not treated as errors — they're informational and the underlying
// call succeeds. Real auth failures still propagate.
func (t *Target) ensureAuth(_ context.Context) error {
	if t.cfg.BotUser == "" {
		// Allow unauthenticated read-only use.
		return nil
	}
	err := t.client.Login(t.cfg.BotUser, t.cfg.BotPass)
	if err == nil {
		return nil
	}
	var warnings mwclient.APIWarnings
	if errors.As(err, &warnings) {
		return nil
	}
	return err
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
//
// Uses action=parse&prop=wikitext&formatversion=2. We bypass
// go-mwclient's jason-based decoding here because the v1 response
// shape has a "*" key (`wikitext.*`) that jason can't navigate.
// formatversion=2 returns wikitext as a plain string and we
// decode with encoding/json directly via GetRaw.
func (t *Target) GetPage(ctx context.Context, title string) (*wiki.Page, error) {
	if err := t.ensureAuth(ctx); err != nil {
		return nil, fmt.Errorf("mediawiki: auth: %w", err)
	}
	raw, err := t.client.GetRaw(params.Values{
		"action":        "parse",
		"page":          title,
		"prop":          "wikitext",
		"formatversion": "2",
	})
	if err != nil {
		return nil, fmt.Errorf("mediawiki: get page: %w", err)
	}
	var resp struct {
		Parse struct {
			Title    string `json:"title"`
			PageID   int    `json:"pageid"`
			Wikitext string `json:"wikitext"`
		} `json:"parse"`
		Error *struct {
			Code string `json:"code"`
			Info string `json:"info"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("mediawiki: decode parse response: %w; body=%s", err, raw)
	}
	if resp.Error != nil {
		if resp.Error.Code == "missingtitle" {
			return nil, nil
		}
		return nil, fmt.Errorf("mediawiki: parse error %s: %s", resp.Error.Code, resp.Error.Info)
	}
	return &wiki.Page{
		Title:   title,
		Content: resp.Parse.Wikitext,
	}, nil
}

// UpsertPage creates or updates a page. bot=true is set so the
// revision shows the bot flag in history.
//
// Returns a Revision populated from what we know at edit time —
// no read-after-write. Immediate readback on MediaWiki can race
// with SQLite indexing on fresh installs and the precise revision
// id isn't worth that fragility. Callers needing the new revision
// id can compose with a follow-up GetPage when stability is critical.
func (t *Target) UpsertPage(ctx context.Context, title, content, summary string) (wiki.Revision, error) {
	if err := t.ensureAuth(ctx); err != nil {
		return wiki.Revision{}, fmt.Errorf("mediawiki: auth: %w", err)
	}
	// MediaWiki csrftokens are session-scoped but some installations
	// (notably fresh SQLite installs) rotate them after each write.
	// Force a fresh fetch before every edit so back-to-back edits
	// don't hit badtoken. Cost: one extra round-trip per edit;
	// correctness is worth it.
	delete(t.client.Tokens, "csrf")
	if err := t.client.Edit(params.Values{
		"title":   title,
		"text":    content,
		"summary": summary,
		"bot":     "1",
	}); err != nil {
		return wiki.Revision{}, fmt.Errorf("mediawiki: edit %q: %w", title, err)
	}
	return wiki.Revision{
		User:      t.cfg.BotUser,
		Timestamp: time.Now().UTC(),
		Comment:   summary,
		IsBot:     true,
	}, nil
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

