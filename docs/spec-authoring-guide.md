# Spec Authoring Guide

A **spec** declares what one wiki page should be. The curator reads
specs, reads the kb, renders pages through the compiler pipeline, and
reconciles human edits. This guide covers what you write; the
architecture is in [`DESIGN.md`](DESIGN.md) (§5 pipeline, §7 spec
model).

A spec file is **YAML frontmatter + a markdown body**:

```markdown
---
wiki: acme
page: Azure_Infrastructure
kind: editorial
version: 1
include:
  areas: [networking, vault, harbor]
  workspaces: [dr, hetzner]
  exclude_zones: [incoming, archived]
fact_check:
  external_truth: quarterly
protected_blocks: [executive-summary]
---

Synthesise the Azure infrastructure story for operators: lead with
the tenant/subscription/identity layer, then networking, then the
service stacks. Keep it skimmable; prefer tables for inventory.
```

## Frontmatter fields

These are the fields the parser actually reads
(`internal/specs/parser`). Anything else is ignored.

| Field | Required | Meaning |
|---|---|---|
| `wiki` | ✅ | Routing **guardrail**. Must match the wiki the run is configured for, or the curator errors loudly (prevents silent mis-routing). Routing itself is config, not this field. |
| `page` | ✅ | Target page title in the wiki. |
| `kind` | ✅ | Frontend selector: `projection`, `editorial`, `hub`, or `runbook`. Unknown kinds are rejected. |
| `version` | — | Spec schema version (integer). |
| `include.areas` | — | kb area IDs this spec may read. Defense-in-depth scope. |
| `include.workspaces` | — | Workspace IDs, or the sentinel string `linked-to-areas`. Accepts a scalar or a list. |
| `include.exclude_zones` | — | kb zones to drop (e.g. `incoming`, `archived`). |
| `fact_check` | — | Opt-in map of `check: cadence`. See below. |
| `protected_blocks` | — | Block IDs the reconciler must never overwrite. |

The **body** (everything after the closing `---`) is the *intent
description* — what the page should accomplish, its voice, ordering
hints. It is **not** the page text. For `projection` specs the body
can be one line; for `editorial` specs it is the brief the LLM
frontend works from.

## Choosing a `kind`

- **`projection`** — deterministic rendering of one or more kb areas.
  Cheap, no LLM, medium quality. Good default for area pages.
- **`editorial`** — LLM-authored narrative from the body brief.
  Higher cost, bespoke quality. Use for hub/topic/cross-cutting
  pages.
- **`hub`**, **`runbook`** — specialised frontends; same frontmatter.

A wiki freely mixes kinds: area pages projection, topic pages
editorial.

## Opting into expensive checks (`fact_check`)

Expensive maintenance checks are **funded by specs that opt in**
(DESIGN §6.4 — pull, not push). If no spec opts an area in, the
curator never spends tokens/web calls on it.

```yaml
fact_check:
  external_truth: quarterly
```

`external_truth` enables the web+LLM external-truth check for **every
kb area in this spec's `include.areas`**. An area nobody opted in is
never externally researched. (The real web-search provider is a v2
deferral — see DESIGN §17; until then the check is wired with a
no-op provider, so opting in is safe and free today.)

## House style

House style (terminology, heading case) is **per-wiki config**, not
per-spec — the top-level `style:` block in the wiki config
(`config.StyleConfig`, applied by the `ApplyStyleRules` pass):

```yaml
style:
  terminology: { k8s: Kubernetes, github: GitHub }
  heading_case: sentence   # "" | sentence | title
```

A per-spec `style:` reference is **not yet implemented**; don't put
one in frontmatter expecting effect.

## Reconciliation & protected blocks

Wikis are *soft-read-only*: humans may edit; the curator detects and
reconciles. Editorial prose is preserved across runs while its inputs
are unchanged; machine blocks are regenerated. List any block IDs you
never want the curator to touch under `protected_blocks`. See
DESIGN §5.6.

## Diagrams

Put mermaid in the kb/spec; the `RenderDiagrams` pass renders it to
PNG and uploads it to the wiki automatically (mermaid is
first-class; other diagram languages pass through untouched). Nothing
spec-side is required beyond the diagram source.

## Checklist before committing a spec

- `wiki:` matches the target wiki's config.
- `kind:` is one of the four known kinds.
- `include.areas` lists every area the page needs — and no more.
- `external_truth` only on areas you actually want externally
  fact-checked (it costs tokens once a provider is wired).
- Body describes *intent*, not page text.
