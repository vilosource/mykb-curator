// Package reconciler decides what to do at the wiki boundary for a
// rendered spec.
//
// Per DESIGN.md §5.6, the reconciler implements the soft-read-only
// contract: humans CAN edit curator-owned wiki pages, the curator
// DETECTS those edits, and the run report ALWAYS surfaces them so
// nothing happens silently. Default policy is overwrite-with-report;
// future spec-level flags (protected: true) will switch a block
// or page to preserve-on-detect.
//
// The reconciler is pure of side effects — it only reads from the
// wiki and computes a Decision. The orchestrator acts on the
// Decision (calling UpsertPage when Action == ActionUpsert).
package reconciler

import (
	"context"
	"fmt"

	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
)

// Action is the disposition the reconciler computes for one (title, content) pair.
type Action string

const (
	// ActionCreate: page does not exist on the wiki; orchestrator
	// will UpsertPage to create it.
	ActionCreate Action = "create"
	// ActionUpsert: page exists and content differs; overwrite.
	ActionUpsert Action = "upsert"
	// ActionNoOp: page exists and content matches; skip the upsert.
	ActionNoOp Action = "noop"
)

// Decision is the reconciler's output for one reconcile call.
type Decision struct {
	Action Action

	// CurrentRevisionID is the wiki's latest revision before this
	// reconcile. Empty for new pages. The orchestrator stamps this
	// into the run report so the next run knows what the curator's
	// last-seen state was.
	CurrentRevisionID string

	// MergedContent is the final content to push to the wiki — the
	// block-level merge of the existing wiki page and the new render
	// (editorial blocks with matching provenance keep their wiki
	// body; machine blocks always use the new render).
	//
	// Empty for ActionNoOp (no push). For ActionCreate it's just the
	// new render verbatim.
	MergedContent string

	// HumanEdits is the list of human edits detected since the last
	// curator write to this page. Empty for new pages, for bot-only
	// histories, or when no human edit was made.
	HumanEdits []HumanEditDetection

	// PreExistingContent indicates the page existed and had non-bot
	// content before the curator first rendered it. Surfaced in the
	// run report so the operator can review whether to take
	// ownership or carve the existing content out.
	PreExistingContent bool
}

// HumanEditDetection describes one detected human edit, ready to
// inline in the run report.
type HumanEditDetection struct {
	Revision wiki.Revision
	Diff     string
}

// Reconciler resolves the wiki state vs the curator's rendered
// output and produces a Decision.
type Reconciler struct {
	target wiki.Target
}

// New constructs a Reconciler bound to a wiki target.
func New(target wiki.Target) *Reconciler {
	return &Reconciler{target: target}
}

// Reconcile fetches the current wiki state and computes the Decision
// for the given (title, rendered bytes, last-known bot rev).
//
// lastBotRevID is the revision ID the curator wrote on the previous
// run; "" indicates first-render or unknown.
func (r *Reconciler) Reconcile(ctx context.Context, title string, rendered []byte, lastBotRevID string) (Decision, error) {
	current, err := r.target.GetPage(ctx, title)
	if err != nil {
		return Decision{}, fmt.Errorf("reconciler: get page %q: %w", title, err)
	}

	// Case 1: page doesn't exist on the wiki → create with new render verbatim.
	if current == nil {
		return Decision{Action: ActionCreate, MergedContent: string(rendered)}, nil
	}

	// Compute the block-level merged content. For editorial blocks
	// with matching provenance hashes the wiki body wins (human
	// polish survives); machine blocks always use the new render.
	merged := MergeBlocks([]byte(current.Content), rendered)

	d := Decision{
		CurrentRevisionID: current.LatestRevision.ID,
		MergedContent:     merged,
	}

	// Case 2: merged content matches current wiki content → no-op.
	// (Either nothing changed or every editorial block was preserved
	// and machine blocks happen to be identical to what's already there.)
	if merged == current.Content {
		d.Action = ActionNoOp
		return d, nil
	}

	// Detect human edits since our last write. If lastBotRevID is
	// empty, walk the whole history and flag any non-bot revs as
	// pre-existing-content (orchestrator decides whether to upsert
	// or skip per spec policy).
	humans, preExisting, err := r.detectHumanEdits(ctx, title, lastBotRevID)
	if err != nil {
		return Decision{}, err
	}
	d.HumanEdits = humans
	d.PreExistingContent = preExisting
	d.Action = ActionUpsert
	return d, nil
}

// detectHumanEdits walks the page's history (newest-first) and
// returns every non-bot revision encountered before reaching
// lastBotRevID. When lastBotRevID is empty, returns nothing but
// sets preExisting=true if any non-bot revision exists.
func (r *Reconciler) detectHumanEdits(ctx context.Context, title, lastBotRevID string) ([]HumanEditDetection, bool, error) {
	hist, err := r.target.History(ctx, title, "")
	if err != nil {
		return nil, false, fmt.Errorf("reconciler: history %q: %w", title, err)
	}
	if lastBotRevID == "" {
		// First-render mode: just flag any human-authored history.
		for _, rev := range hist {
			if !rev.IsBot {
				return nil, true, nil
			}
		}
		return nil, false, nil
	}
	var out []HumanEditDetection
	for _, rev := range hist {
		if rev.ID == lastBotRevID {
			break
		}
		if !rev.IsBot {
			out = append(out, HumanEditDetection{
				Revision: rev,
				Diff:     fmt.Sprintf("revision %s by %s: %s", rev.ID, rev.User, rev.Comment),
			})
		}
	}
	// The Diff field on HumanEditDetection is a coarse summary for
	// now. A real diff against the prior bot revision lands when the
	// reconciler is extended with content-level diffing in v0.5.
	return out, false, nil
}
