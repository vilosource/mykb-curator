package reconciler

import (
	"context"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/memory"
)

func TestReconcile_PageDoesNotExist_PushesAsCreate(t *testing.T) {
	tgt := memory.New("User:Bot")
	r := New(tgt)

	dec, err := r.Reconcile(context.Background(), "NewPage", []byte("new content"), "")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if dec.Action != ActionCreate {
		t.Errorf("Action = %q, want %q", dec.Action, ActionCreate)
	}
	if len(dec.HumanEdits) != 0 {
		t.Errorf("HumanEdits = %d, want 0 (new page has no history)", len(dec.HumanEdits))
	}
}

func TestReconcile_NoChanges_NoOp(t *testing.T) {
	// If the current wiki page is byte-equal to the rendered output,
	// no upsert is needed — saves a no-op wiki revision.
	tgt := memory.New("User:Bot")
	rev, _ := tgt.UpsertPage(context.Background(), "P", "content", "first")

	dec, _ := New(tgt).Reconcile(context.Background(), "P", []byte("content"), rev.ID)
	if dec.Action != ActionNoOp {
		t.Errorf("Action = %q, want %q (content matches; no upsert needed)", dec.Action, ActionNoOp)
	}
}

func TestReconcile_BotOnlyHistory_UpsertWithoutFlaggingHumans(t *testing.T) {
	tgt := memory.New("User:Bot")
	rev, _ := tgt.UpsertPage(context.Background(), "P", "old", "first")

	dec, _ := New(tgt).Reconcile(context.Background(), "P", []byte("new"), rev.ID)
	if dec.Action != ActionUpsert {
		t.Errorf("Action = %q, want %q (content changed)", dec.Action, ActionUpsert)
	}
	if len(dec.HumanEdits) != 0 {
		t.Errorf("HumanEdits = %d, want 0 (no human edits since last bot rev)", len(dec.HumanEdits))
	}
}

func TestReconcile_HumanEditDetected_FlaggedAndStillUpserts(t *testing.T) {
	// The "soft read-only" contract: when humans edit a curator-owned
	// page, the curator detects + overwrites + records the event.
	// Override semantics (preserve-on-detect) is a future spec-level
	// flag; default policy here is overwrite-with-report.
	tgt := memory.New("User:Bot")
	rev, _ := tgt.UpsertPage(context.Background(), "P", "v1-bot", "first")
	tgt.SimulateHumanEdit("P", "User:Alice", "v1-human", "drive-by fix")

	dec, _ := New(tgt).Reconcile(context.Background(), "P", []byte("v2-bot"), rev.ID)
	if dec.Action != ActionUpsert {
		t.Errorf("Action = %q, want %q (overwrite-with-report)", dec.Action, ActionUpsert)
	}
	if len(dec.HumanEdits) != 1 {
		t.Fatalf("HumanEdits = %d, want 1", len(dec.HumanEdits))
	}
	e := dec.HumanEdits[0]
	if e.Revision.User != "User:Alice" {
		t.Errorf("HumanEdits[0].User = %q, want %q", e.Revision.User, "User:Alice")
	}
}

func TestReconcile_NoBotRevYet_TreatsAsCreate(t *testing.T) {
	// First-ever render of a page (lastBotRevID == ""): if the page
	// doesn't exist, it's a create. If it does exist but we have no
	// record of writing it, we treat it as a create too — but flag
	// any existing content as a "pre-existing" event so the operator
	// can review.
	tgt := memory.New("User:Bot")
	tgt.SimulateHumanEdit("P", "User:Alice", "human-authored content", "wrote this page")

	dec, _ := New(tgt).Reconcile(context.Background(), "P", []byte("curator content"), "")
	if dec.Action != ActionUpsert {
		t.Errorf("Action = %q, want %q (pre-existing page, take ownership)", dec.Action, ActionUpsert)
	}
	if !dec.PreExistingContent {
		t.Errorf("PreExistingContent = false, want true (page existed before curator's first render)")
	}
}

func TestReconcile_HumanEditEvents_IncludeRevisionMetadata(t *testing.T) {
	// v0.0.4 scope: the Diff summarises the human revision (user,
	// id, edit summary). Content-level diff against the prior bot
	// revision is a v0.5 extension — see reconciler.go inline note.
	tgt := memory.New("User:Bot")
	rev, _ := tgt.UpsertPage(context.Background(), "P", "original", "first")
	tgt.SimulateHumanEdit("P", "User:Alice", "edited by human", "drive-by tweak")

	dec, _ := New(tgt).Reconcile(context.Background(), "P", []byte("new content"), rev.ID)
	if len(dec.HumanEdits) == 0 {
		t.Fatalf("expected human edit detected")
	}
	d := dec.HumanEdits[0].Diff
	if !strings.Contains(d, "User:Alice") {
		t.Errorf("Diff missing editor identity: %q", d)
	}
	if !strings.Contains(d, "drive-by tweak") {
		t.Errorf("Diff missing edit summary: %q", d)
	}
}

// stubFailingTarget surfaces errors at known callsites.
type stubFailingTarget struct {
	wiki.Target
	failOnGetPage bool
}

func (s stubFailingTarget) GetPage(_ context.Context, _ string) (*wiki.Page, error) {
	if s.failOnGetPage {
		return nil, errStub
	}
	return nil, nil
}
func (s stubFailingTarget) HumanEditsSinceBot(context.Context, string, string) (*wiki.HumanEdit, error) {
	return nil, nil
}

var errStub = stubErr("simulated wiki failure")

type stubErr string

func (e stubErr) Error() string { return string(e) }

func TestReconcile_WikiGetFails_PropagatesError(t *testing.T) {
	r := New(stubFailingTarget{failOnGetPage: true})
	_, err := r.Reconcile(context.Background(), "P", []byte("x"), "")
	if err == nil {
		t.Errorf("expected error from failing GetPage, got nil")
	}
}
