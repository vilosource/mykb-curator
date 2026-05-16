// Package wiki defines the WikiTarget adapter interface and the
// data types it exchanges with the rest of the curator.
//
// Implementations live in subpackages (mediawiki, markdown,
// confluence). All implementations must satisfy the contract test
// suite in test/contract.
package wiki

import (
	"context"
	"time"
)

// Target is the boundary between the curator and a wiki backend.
//
// The interface is intentionally narrow: read pages and history,
// write pages, detect human edits. Everything wiki-specific lives
// inside the implementation.
type Target interface {
	// Whoami returns the bot identity the adapter is authenticated as.
	// Used by HumanEditsSinceBot to distinguish bot writes from
	// human edits.
	Whoami(ctx context.Context) (string, error)

	// GetPage fetches the current state of a page. Returns nil if the
	// page does not exist (not an error).
	GetPage(ctx context.Context, title string) (*Page, error)

	// UpsertPage creates or updates a page with the given content.
	// The summary is the wiki edit summary (recorded in history).
	// Implementations should set bot=1 when supported.
	UpsertPage(ctx context.Context, title, content, summary string) (Revision, error)

	// History returns revisions of a page, optionally bounded by a
	// since-revision-id.
	History(ctx context.Context, title string, sinceRevID string) ([]Revision, error)

	// HumanEditsSinceBot reports whether any human (non-bot) revision
	// occurred on a page after the given bot revision. Load-bearing
	// for the reconciler's soft-read-only contract.
	HumanEditsSinceBot(ctx context.Context, title string, lastBotRevID string) (*HumanEdit, error)

	// UploadFile uploads a binary asset (e.g. a rendered diagram) to
	// the wiki under the given filename and returns the reference a
	// backend embeds to display it (implementation-defined form —
	// e.g. a MediaWiki "File:Name.png" title). Re-uploading identical
	// content under the same filename must be idempotent (no error,
	// same ref). Used by the RenderDiagrams pass.
	UploadFile(ctx context.Context, filename string, content []byte, contentType, summary string) (assetRef string, err error)
}

// Page is a wiki page snapshot.
type Page struct {
	Title          string
	Content        string
	LatestRevision Revision
}

// Revision is one historical version of a page.
type Revision struct {
	ID        string
	User      string
	Timestamp time.Time
	Comment   string
	IsBot     bool
}

// HumanEdit describes a human modification detected since the last
// bot write.
type HumanEdit struct {
	Revision Revision
	// Diff is a wiki-syntax diff between the last bot rev and the
	// current human-edited content. Format is implementation-defined;
	// consumers should treat it as opaque text for run-report display.
	Diff string
}
