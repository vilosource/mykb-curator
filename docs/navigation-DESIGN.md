# DESIGN — auto-derived navigation (self-registering hubs)

> Status: **IN PROGRESS (2026-05-30). Slices 1–5 DONE + live-verified on
> the personal wiki** — Main Page routing, placement model, self-
> registering `members: auto` hubs, `nav.area` blurbs, supersede-via-
> redirect, and the `Azure_Infrastructure` + `gitlab-runners` cluster
> migrations. Commits 034329e→48779d0 on main; full pyramid green.
> **Remaining: general orphan-pruning (§11).** Supersedes the discarded
> `site-index-DESIGN.md` draft, whose premise (no index existed) was
> false.

---

## 1. Problem (corrected) & principle

### 1.1 What's actually true

- **`Main Page` is untouched MediaWiki boilerplate** ("MediaWiki has
  been installed…") linking to *nothing* in the wiki. That is the
  visible navigation gap.
- **A spec-driven index hierarchy already exists and is live**, built
  from `kind: hub` specs: `OptiscanGroup` → `OptiscanGroup/Azure_
  Infrastructure` → 24 `leaf-*` area pages. But the hubs are
  **hand-authored** — `azure-infrastructure.spec.md` contains an
  explicit `hub.sections[].links[]` list with hand-typed labels,
  blurbs and section groupings. Add a `leaf-foo.spec.md` and you must
  hand-edit the hub to link it. **That hand-maintenance is the drift to
  eliminate.**
- The **docspec clusters** (`vault.doc.yaml` → flat `Vault
  Architecture`; scoped `gitlab-runners.doc.yaml`) live *outside* the
  hub hierarchy entirely — orphaned from navigation.

### 1.2 The principle (operator intent, 2026-05-30)

> **Navigation is fully curator-owned and auto-derived — exactly like
> page content. A human never hand-maintains a link list or edits
> rendered output. All human control, including bespoke per-page
> treatment, is expressed declaratively in a spec; the curator
> realises it.**

This is the curator's existing `child-index` discipline — *"the spec
owns order/structure; the entries auto-fill"* (DESIGN.md §5.2,
`architecture.go:76`) — **generalised from within-one-cluster to the
whole site hub hierarchy.**

---

## 2. Decisions (resolved in session 2026-05-30)

| # | Decision | Rationale |
|---|---|---|
| **N1** | **Hubs are self-registering, not hand-listed.** Each page's spec declares *where it belongs*; hubs **auto-collect** the specs that point at them. | Kills hand-maintenance: add a spec → it appears automatically. The hub spec keeps *structure* (section order, intro) but not *membership*. |
| **N2** | **Placement is declared (a `nav` block), with subpath-derived defaults.** | Declared = flexible, decouples nav from URL, lets flat clusters slot in, supports multi-placement, and is the "custom goes in the spec" escape hatch. Subpath default = zero-maintenance when no override is needed. They compose. |
| **N3** | **Completeness is guaranteed.** A page with no `nav` and no usable subpath still appears (default "Uncategorised" bucket); ordering falls back to deterministic alphabetical. | Nothing is ever silently dropped — same contract as `child-index`. |
| **N4** | **Leaf vs cluster = supersede.** Where a docspec cluster covers the same kb area as a leaf, the cluster is canonical and the leaf is **retired via redirect** to the cluster parent. | One canonical page per topic; anti-drift. Leaf = lightweight default, cluster = declarative upgrade. |
| **N5** | **`Main Page` routes to the top hub** (curator-owned), never hand-edited. | Closes the actual gap; the top hub is the landing index. |
| **N6** | **v1 phasing: build the mechanism + prove on one hub; migrate the rest after.** General orphan-pruning is a tracked follow-on, not v1. | Smallest correct increments; avoid a 26-spec big-bang. |

Both N2 and N4 share one shape — **lightweight default, declarative
upgrade** (subpath→declared; leaf→cluster) — a good sign the model is
coherent.

---

## 3. Model

### 3.1 Self-registering hubs (the inversion)

- **Today (push):** a hub spec holds an explicit `links:` list.
- **Goal B (pull):** each page spec declares `nav.parent: <hub page>`;
  the hub frontend **auto-collects** every spec whose `nav.parent`
  resolves to it, groups by `nav.section`, orders by `nav.order` then
  title, and renders the link list. The hub spec declares only section
  order + intro prose.

Mechanism: the orchestrator builds a **nav map** — `parent → [ {page,
section, order, label, blurb} ]` — from every spec's placement, and
feeds it to the hub frontends. One nav map, consumed by all auto-hubs.

**Two spec systems + an ordering refactor (self-review 2026-05-30).**
Placement must be declared on **both** spec types — doc specs
(`DocSpec`, cluster path) *and* leaf specs (`Spec`, registry path) —
which are **different Go types with different parsers**, so `nav` is
added to both. And today the orchestrator renders the legacy
`specList` **before** it pulls docspecs; auto-hubs need the **complete**
nav map (both systems) **before any hub renders**. So the run becomes:
**pull all specs → build nav map → resolve supersede → render** (hubs
included). This is a real orchestrator refactor, not a hook.

**Auto-hubs are exempt from diff-skipping (self-review 2026-05-30).** A
hub's content depends on its *members*, not its own spec, so a
diff-driven run that adds/removes a member wouldn't otherwise re-render
the hub → stale. Auto-hubs (and the superseded-leaf redirects) **always
re-derive**, regardless of `DiffSince`.

**Hubs self-register too.** A hub spec may itself declare `nav.parent`,
so the root hub auto-lists sub-hubs (recursion). This requires: a
**root-identification** rule (the `kind: index` page, or the hub with no
parent — see §3.3/§7), and **validation** that every `nav.parent`
resolves to an existing hub with **no cycles**.

### 3.2 Placement: declared `nav` block + subpath default

Per-page spec (works for both `.doc.yaml` parents and `.spec.md`):

```yaml
nav:
  parent: OptiscanGroup/Azure_Infrastructure   # which hub
  section: Core Infrastructure                  # group within the hub
  order: 30                                     # sort weight (then title)
  label: Networking & Connectivity              # display label
  blurb: One-line description for the hub list.
```

Resolution order per field:
1. explicit `nav.<field>` if present;
2. else **derive from the subpage path** (`parent` = the title's parent
   path; `label` = last segment, de-underscored);
3. else default (no section; alphabetical order; `blurb` falls back to
   the kb `area:` summary — the existing `hub` `area:` mechanism — or
   the parent `intent`'s first sentence).

A page with neither `nav` nor a subpath parent → "Uncategorised"
bucket (N3). The blurb is deliberately **short** (one line); long
`intent` paragraphs are truncated to the first sentence unless an
explicit `blurb` is given.

### 3.3 Hub spec = structure + intro, membership auto-derived

A hub gains an **auto-membership mode** (vs. today's explicit list,
kept for fully-manual hubs / back-compat):

```yaml
kind: hub            # intermediate hub
page: OptiscanGroup/Azure_Infrastructure
hub:
  members: auto      # collect specs whose nav.parent == this page
  sections:          # declares ORDER + intro only, not links
    - { title: Core Infrastructure, desc: "Focus: the base layer…" }
    - { title: Platform Service Automation, desc: "…" }
```

Open taxonomy choice (see §7): keep a distinct **`kind: index`** for the
single **site-root** auto-hub that `Main Page` routes to, vs. treat the
root as just the top `kind: hub`. Lean: use the reserved `kind: index`
for the root landing hub; `kind: hub` for intermediate auto-hubs.

### 3.4 Leaf vs cluster: supersede via retire-redirect

The cluster parent is canonical over a leaf it supersedes:
- the **nav map** lists the cluster parent, not the leaf;
- the **leaf page renders as a redirect** — `#REDIRECT [[<cluster
  parent>]]` — so existing URLs/links/bookmarks survive.

**Supersede must be PRECISE (self-review 2026-05-30).** A cluster spans
*multiple* kb areas (e.g. `vault.doc.yaml`'s DR section sources
`kb:area=disaster-recovery` **and** `kb:area=vault`). Keying supersede
on *any shared area* would wrongly retire `leaf-disaster-recovery` into
`Vault Architecture`. So supersede is keyed on the cluster's **primary
(topic) area only, or an explicit `supersedes: <leaf>`** — never "any
shared area." Lean: explicit `supersedes:`, defaulting to the topic
area when unambiguous.

**Render-vs-redirect resolution.** A superseded leaf's spec is
**retained**, but the curator emits the redirect **instead of** the
leaf's projected content — otherwise it would render content to the
leaf page every run and fight the redirect. So supersede is **resolved
before rendering**: a superseded leaf renders a redirect, not its
projection. Un-superseding (drop the relationship) restores content.

**This needs a new, small capability** (verified absent 2026-05-30): the
wiki `Target` has `UpsertPage` but **no delete, no redirect, no
page-retirement**. A *retire-via-redirect* path:
1. `UpsertPage(leafTitle, "#REDIRECT [[<cluster parent>]]", summary)` —
   a redirect is just page content; **no MediaWiki delete rights
   needed**;
2. **bypasses the block-merge reconciler** (which merges blocks, and
   can't turn a content page into a bare first-line `#REDIRECT`) —
   writes raw redirect content;
3. **guarded by `HumanEditsSinceBot`** — never redirect over a
   human-edited leaf (honour the soft-read-only contract); on conflict,
   skip + warn in the run report;
4. **idempotent** — compare current page content to the desired
   redirect and **skip the write when already correct** (the path
   bypasses the reconciler's no-op detection, so it must do its own).

This is *targeted* retirement (a known superseded leaf), **not** general
orphan-pruning (pages from deleted/renamed specs) — see §9.

### 3.5 `Main Page` → top hub

`Main Page` becomes curator-owned: route it to the site-root hub.
Cheapest→richest:
1. set `$wgMainPage` to the root hub page (wiki config, not curator); or
2. curator owns a `Main Page` block: *"Start at [[OptiscanGroup]]"*; or
3. `Main Page` transcludes `{{:<root hub>}}`.

If transcluding (3), the root hub must wrap its `[[Category:…]]` and
`CURATOR:` markers in `<noinclude>` so they don't leak into `Main Page`
(MediaWiki gotcha). Lean: option 2 (a small curated link block) — robust,
no transclusion edge cases, curator-owned.

---

## 4. New capabilities required

1. **Nav placement fields** — parse `nav: {parent, section, order,
   label, blurb}` on **both** `DocSpec` and `Spec` (separate parsers);
   subpath-derivation helper. `blurb` is an explicit short string;
   fall back to the kb `area:` summary, **not** an auto-truncated
   `intent` (sentence-splitting is fragile).
2. **Orchestrator refactor** — pull all specs → build nav map (both
   systems) → resolve supersede → render; auto-hubs + superseded-leaf
   redirects exempt from `DiffSince`. Validate `nav.parent` resolves +
   no cycles + single root.
3. **Nav map + auto-membership hubs** — hub frontend gains
   `members: auto` (collect by `nav.parent`, group by section,
   deterministic order). Reuses `ir.IndexBlock` + `ValidateLinks`.
4. **Retire-via-redirect** — page-retirement path (§3.4): idempotent raw
   redirect `UpsertPage` + `HumanEditsSinceBot` guard + **precise**
   supersede (`supersedes:` / topic area, not any shared area).
5. **`Main Page` routing** (§3.5, option 2).

---

## 5. Migration (existing hand-authored hubs → self-registering)

The curated labels/blurbs/section-groupings currently in
`OptiscanGroup.spec.md` / `azure-infrastructure.spec.md` **move into the
individual page specs** (each `leaf-*` declares its own `nav`). The
hubs become thin (structure + intro, `members: auto`). Phased, not
big-bang (N6): prove on `Azure_Infrastructure` first, then migrate the
rest leaf-by-leaf. Back-compat: explicit-list hubs keep working until
migrated.

---

## 6. Determinism, idempotency, reconciler, links

- **Deterministic**: nav map → sorted (section order, then `order`,
  then title). No LLM, no volatile content (no timestamps/counts) →
  reconciler no-op when the spec set is unchanged.
- **ValidateLinks**: hub links target pages created in the same run
  (precedent: `child-index`). **Verify** (§7) it tolerates same-run
  targets, `[[:Category:X]]` namespace links, and that a retired leaf's
  redirect doesn't register as a broken link.
- **Broken-link safety**: a hub should link only pages that exist or are
  produced this run; if a member cluster failed to render, omit it +
  warn (don't emit a red link).

---

## 7. Open questions / verification items

1. **`kind: index` vs `hub members: auto` for the root** — taxonomy
   choice (§3.3). Decide at slice 2.
2. **ValidateLinks** vs same-run targets, `Category:` links, and
   redirect pages (§6).
3. **Subpath parent resolution** for flat-titled cluster pages — they
   have no subpath, so they *must* declare `nav.parent` (fine; reinforces
   declared placement).
4. **Reconciler bypass for redirect writes** — confirm a raw
   `UpsertPage` outside the block-merge path is acceptable and that the
   reporter records it.
5. **Supersede derivation** — by shared kb `area` vs explicit
   `supersedes:`. Lean: derive, with explicit override.

---

## 8. Implementation plan (slices)

Resequenced (self-review 2026-05-30): front-load the visible win, then
build the auto-derivation mechanism (B2) on the path to full Goal B (B1).

1. **`Main Page` routing — INSTANT VALUE, zero new code.** Author a
   `main-page` `kind: hub` spec (`page: Main Page`) linking to the
   existing root hub `OptiscanGroup` (which already auto-indexes the 24
   leaves). Uses the existing deterministic hub frontend. Live-verify:
   `Main Page` links to `OptiscanGroup`. This alone closes the original
   "can't reach the area pages" gap. (Handle the default Main-Page
   boilerplate; confirm ValidateLinks accepts the live `OptiscanGroup`
   target.)
2. **Placement model (L1).** Parse `nav` fields on **both** `DocSpec`
   and `Spec`; subpath-derivation helper; defaults + "Uncategorised";
   explicit-`blurb` rule. Pure-function red/green unit tests. No wiring.
3. **Orchestrator refactor + nav map + auto-hub — B2, proven on ONE
   hub.** Pull-all → build nav map → render; `members: auto` hub mode;
   parent/cycle/root validation; auto-hubs exempt from `DiffSince`.
   Migrate **only `Azure_Infrastructure`** to `members: auto`, relocating
   its leaves' labels/blurbs into their specs. L1/L2/L3 + a live confirm
   the hub auto-lists the leaves. (Decide root taxonomy, Q1.)
4. **Retire-via-redirect + precise supersede (toward B1).** The
   idempotent retirement path (§3.4) + `HumanEditsSinceBot` guard +
   explicit `supersedes:`; redirect `…/Vault` → `Vault Architecture`;
   render-vs-redirect resolution; live-confirm the redirect resolves.
5. **Complete B1.** Migrate remaining hubs/leaves to self-registration;
   route `Main Page` to the auto-root; retire the rest of the
   superseded leaves. Incremental (N6).

Follow-on (out of scope, §9): general orphan-pruning.

Process: TDD red/green, `-race`, gofmt + golangci-lint v2 + go mod tidy
clean, conventional commits, feature branch ff-merged. DONE BAR = a live
run on the personal harness, not just replay.

---

## 9. Out of scope (follow-ons)

- **General orphan-pruning** — when a spec is deleted/renamed, auto-
  retire the old page. The curator has **none** of this today (no page
  inventory across runs, no delete). Supersede needs only *targeted*
  retirement (§3.4); the general pruner is a larger, separate feature —
  **tracked follow-on**, the remaining piece of a fully hands-off Goal B.
- **MediaWiki page DELETE** (vs redirect) — would need delete rights +
  new API; not required for supersede.
- **Sidebar (`MediaWiki:Sidebar`) persistent nav** — optional later.
- **Curated `Category:`-page leads** — optional later.

---

## 11. Orphan-pruning (the last follow-on) — design

**Problem.** When a spec is *deleted or renamed*, the page it used to
produce lingers on the wiki forever (the curator has no page inventory
across runs and no delete). For fully hands-off Goal B, a removed spec
should auto-retire its page.

**Page inventory.** Maintain a manifest of `{specID → page}` the curator
produced (RunState already keys per-spec; add the page title, or a
single produced-pages record). Each run computes `producedThisRun`.

**Orphan detection — by ABSENCE, not failure.** An orphan is a page in
the manifest whose **owning spec ID is no longer in the run's spec set**
(genuinely removed/renamed). Critically, a spec that merely *failed* to
render this run is **not** an orphan — otherwise a transient render
error would wrongly retire a live page. This guard is load-bearing.

**Retire action (the fork, lean noted):**
- **(a) Redirect to the subpath parent hub** *(lean)* — a subpaged
  orphan (`…/Azure_Infrastructure/Foo`) becomes `#REDIRECT
  [[…/Azure_Infrastructure]]`, so old URLs land on the parent index.
  Reuses the §3.4 retire-via-redirect path (idempotent + human-edit
  guard, no delete rights). A flat orphan (no subpath) has no obvious
  target → fall back to (b).
- **(b) Deprecation stub** — replace content with a "no longer
  maintained" banner + a tracking category; it drops out of hubs
  automatically (its spec is gone, so it's already out of the nav map).
- **(c) MediaWiki delete** — cleanest semantically, but needs delete
  rights + a new `Target.Delete`. Deferred.

**Safety:** human-edit guard (don't retire a human-edited page);
idempotent (skip when already retired); the manifest update is the
record of what's now owned.

**Status:** designed, not built. The retire mechanism (§3.4) already
exists; orphan-pruning adds the inventory + absence-detection on top.

---

## 10. Principle to capture (kb decision / memory, on approval)

Navigation is curator-owned and **auto-derived**; humans edit **specs,
not rendered output**; bespoke treatment is **declarative** in the spec.
Hubs self-register (children declare placement; hubs auto-collect).
Leaf = lightweight default; cluster = canonical upgrade (leaf retired
via redirect). General orphan-pruning is the outstanding gap.
