# Doc-Spec Format — Spec-Driven Development for Documentation

**Status: agreed direction (2026-05-16), implementation sliced
below.** This is the authoritative definition of the doc-spec
language. Grounded in the real hand-crafted
`wiki.optiscangroup.com/Vault Architecture` page.

## Why

Today's pages are machine-oriented projections: the `projection`
frontend dumps an area's facts/decisions/gotchas/patterns/links 1:1.
That is a *reference dump*, not a curated human document. The goal:
the curator (an LLM — me, ad hoc, today; the curator AI later)
produces the same kind of **human-curated, narrative,
architecture-oriented pages** an engineer hand-writes — via a
**spec**, exactly like Spec-Driven Development, except the build
target is documentation, not code. The IR/LLVM compiler shape we
built is the substrate: doc-spec (source) → frontend → IR → passes →
backend → wiki page ("binary"). The Judge is the test; the
clarification interview authors the spec.

## Model

A topic is a **cluster**: a parent page + child pages, cross-linked
("Part of:" on children, a generated child-index on the parent),
mirroring the real wiki (e.g. Vault Architecture + Vault Operations
+ Vault DR Restore + Vault JWT Auth Reference + Vault Ansible
Integration + a machine-only Vault Reference).

```yaml
topic: Vault
parent:
  page: OptiscanGroup/Azure_Infrastructure/Vault_Architecture
  kind: architecture          # architecture|runbook|reference|integration|index
  audience: human-operator    # human-operator|newcomer|llm-reference
  intent: >                   # the page-level acceptance contract
    A human understands what Vault is, where it runs, how it is
    built, how to reach it, how it is recovered — without reading kb.
  sections:
    - title: System Architecture
      intent: Platform, storage backend, auto-unseal, network, identity/SSO.
      sources: ["kb:area=vault tag=ha,raft,unseal,network,auth"]
    - title: Deployment & Operations
      intent: Stack deployment, secret sync, backup strategy.
      sources: ["kb:area=vault tag=deploy,backup", "kb:area=backup"]
    - title: Disaster Recovery
      intent: Recovery model + phased restore procedure.
      sources: ["kb:area=vault tag=dr", "kb:area=disaster-recovery"]
    - title: Source Code & IaC
      render: table                                  # Component|Repo|Path
      sources: ["git:infra-docker-stacks/hashicorp-vault"]
    - title: Operational Runbooks
      render: child-index                            # generated from children
  related: [Docker_Swarm_Platform, Disaster_Recovery, Wildcard_SSL_Automation]
  categories: [Azure Infrastructure, Infrastructure Services Stacks, Security, Vault]
children:
  - {page: .../Vault_Operations,        kind: runbook,     audience: human-operator, intent: "...", sources: [...]}
  - {page: .../Vault_JWT_Auth_Reference, kind: reference,   intent: "...", sources: [...]}
  - {page: .../Vault_Ansible_Integration, kind: integration, intent: "...", sources: [...]}
  - {page: .../Vault_DR_Restore,        kind: runbook,     intent: "7-phase restore", sources: [...]}
  - {page: .../Vault_Reference,         kind: reference,   audience: llm-reference, sources: ["kb:area=vault"]}
```

### Semantics

- **`intent` (page + per-section)** = the SDD acceptance criteria:
  what the content must convey. The LLM synthesises to satisfy it;
  the Judge later validates the rendered section against it.
- **`sources` (per section)** = declared provenance. Scheme-tagged:
  - `kb:area=<id> [tag=a,b] [zone=...]` — implemented today.
  - `git:<repo/path>`, `cmd:<az ...>`, `ssh:<host> <cmd>`,
    `file:<path>` — **reserved**; resolved by the future
    tool-using curator (the reality-probe family). Until then a
    human (me) fills them ad hoc. The spec always declares *where
    truth comes from* per section.
- **`audience`** is the human-vs-machine lever: `human-operator`/
  `newcomer` → curated narrative; `llm-reference` → the 1:1 dump.
  The dump survives, but as a `kind: reference` **child**, never
  the parent.
- **`render`** lets one page mix modes: prose sections +
  `render: table` + `render: child-index` — the hybrid the real
  pages actually are.
- **topic→cluster**: one doc-spec emits parent + children;
  cross-links (`Part of:`, parent child-index, `related`,
  `categories`) are generated, never hand-maintained.

### SDD loop

Author spec via interview (ad hoc me first; a Claude Code skill
later) → spec is the durable artifact → curator regenerates the
cluster every run → LLM synthesises each section from its declared
sources to satisfy `intent` → Judge validates rendered-vs-intent →
reality-probe fills/validates `git:/cmd:/ssh:`. Iterate the spec,
not the page.

## Implementation slices

0. **This doc** — agreed format recorded (done).
1. **DocSpec model + parser + validation** — freeze the language;
   no rendering yet. The contract everything else consumes.
2. **`architecture` frontend** consuming per-section intent+sources
   (kb only today), audience-aware narrative; **projection demoted**
   to the `reference`-child generator.
3. **topic→cluster orchestration** — one DocSpec → parent + N
   children; generated cross-links; `render: table|child-index`.
4. *(later, depends on 1–3)* `git:/cmd:/ssh:` source resolvers
   (reality-probe family) + Judge validating section-vs-`intent`.

Deltas from today: `editorial` is the thin seed (no per-section
intent/sources, single brief) — superseded by the `architecture`
frontend. `projection` keeps existing as the reference generator.
IR block kinds (Prose/Index/Table/Marker) already suffice — the
LLVM shape pays off; this is a richer frontend + spec language, not
an IR rethink.
