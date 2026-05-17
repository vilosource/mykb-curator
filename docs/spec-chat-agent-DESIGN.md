# DESIGN — spec-chat agent-service (Track A)

> Status: **SHIPPED 2026-05-17 — D1–D7 resolved; slices 1–5 on
> `main`, full four-level pyramid green.** Slice 1 docedit
> (fidelity .doc.yaml editor); slice 2 curator-api + specchat +
> `serve`; slice 3 Node agent-service (5 tools + composed gate);
> slice 4 pi-web-ui browser surface (D7a); slice 5 the GRS-gap
> capstone scenario (Judge verdict FLIPS fail→pass through the real
> pipeline once the widen-sources remedy grounds the claim).
> **Live-UX CLOSED 2026-05-17:** the shipped v1 surface (pi-web-ui-
> styled transcript + D2/D6 approval panel over RemoteAgent) was
> driven in a REAL browser (Playwright, spike-04's method) against
> the real built SPA + real static server, Agent/LLM stubbed
> (deterministic, no OAuth): SPA renders; a turn round-trips through
> `/chat`; the full propose → "Approval required (D2/D6)" panel →
> Approve (`/approve` round-trip) → auto re-prompt → "Applied" loop
> works end to end. Repeatable via `agent-service/scripts/
> smoke-web.mjs` (see §6). The earlier "build-verified only" caveat
> is resolved for what v1 ships. Full `ChatPanel.setAgent` rich
> tool/streaming rendering remains a deliberate **v2 enhancement**
> (explicitly out of v1 scope per D7), NOT a carried defect.
> Foundations: `github.com/vilosource/pi-sdk` `pattern/PATTERN.md`
> (the agentic-workflow pattern) + mykb-curator internal packages
> (verified surface map, 2026-05-17).
> Goal (original ask): *open a wiki page, chat with an agent to
> improve that page's `.doc.yaml` spec, see render+Judge feedback,
> apply the spec change* — closing GRS-type gaps interactively.

---

## 1. Why three processes (this is forced, not a choice)

The curator is **Go, CLI-only, no HTTP server**; its four needed
capabilities (read a kb area, read/write a `.doc.yaml`, dry-run
render, Judge) are **internal Go packages**, not CLI subcommands.

The pi-sdk pattern is categorical: a non-Node consumer ⇒ run a Node
agent service and talk **HTTP/SSE**; **do NOT shell the CLI + scrape**
(PATTERN §1.2, [KU2/E2]). That rule applies symmetrically to the
inverse direction here: the Node agent must **not** shell the Go
`mykb-curator` binary and parse stdout. Therefore:

```
[browser: @earendil-works/pi-web-ui ChatPanel]
        │  HTTP/SSE  (NO credentials in browser)            [PATTERN §2]
        ▼
[Node agent-service]  — pi SDK
  • AuthStorage(~/.pi/agent) Copilot OAuth, server-side       [E1]
  • Agent (agent-core), session-per-request, dispose()        [E2]
  • ModelRegistry resolve+validate; 0x gpt-4.1 dev default    [E1/KU6]
  • noTools:"all" + customTools + composed beforeToolCall gate [KU7/E3]
  • customTools = thin JSON/HTTP clients ↓
        │  HTTP/JSON  (localhost; structured contract)
        ▼
[Go curator-api]  — NEW thin adapter over EXISTING engine
  • read_kb_area      → adapters/kb (local.Source.Pull)
  • get_doc_spec      → specs/docspec.Parse + docspecs/localfs
  • put_doc_spec      → NEW faithful serializer (gated; §5 D2)
  • preview_spec      → cluster.Render + judge.Review (one shot)
```

The Go side stays the engine; `curator-api` is one more **adapter**
in the existing hexagonal boundary (`internal/adapters/…`) — it adds
no domain logic, only an HTTP transport over packages the `run`
command already composes (`cmd/mykb-curator/main.go:749`).

## 2. The four custom tools (agent-visible surface)

`defineTool` per PATTERN §3; `noTools:"all"` sandbox; every tool is a
localhost HTTP call to `curator-api`. Mutations gated in
`beforeToolCall` (PATTERN §3, [E3]).

| tool | reads/mutates | Go backing (verified file:line) |
|---|---|---|
| `read_kb_area(area)` | read | `internal/adapters/kb/local/local.go:60` `Pull` → filter Area |
| `get_doc_spec(id)` | read | `internal/specs/docspec/docspec.go:93` `Parse` via `docspecs/localfs` |
| `put_doc_spec(id, spec)` | **mutate spec** | NEW serializer (§5 D2); gated. Covers *widen-sources* (editing a section's `sources:` to pull another `area=`) — no separate tool. |
| `propose_kb_entry(area, type, text, source, why?)` | **mutate brain** | §5 D6 — closes brain-content gaps (e.g. add the GRS Raft-snapshot fact to `area=vault`). Gated; provenance-mandatory; `incoming` zone. |
| `preview_spec(id, spec?)` | read (paid LLM) | `cluster.Render` + `judge.Review` (`internal/judge/judge.go:80`), grounding via `architecture.SectionGrounding` (`…/architecture.go:270`) |

`preview_spec` is **one composite tool** (render→judge→diff in one
call, candidate spec optional = preview an unsaved edit) so the agent
cannot propose a `put_doc_spec` without the Judge having seen it. It
returns `{ markdown, diff_vs_current, verdicts[], ungrounded_claims[] }`.

## 3. Two LLM layers (cost model)

- **Agent layer (Node, pi SDK):** conversational orchestration only —
  0x `github-copilot/gpt-4.1`, registry-resolved, `usage.cost` per
  call (PATTERN §3 cost rule). No premium spend for chat steering.
- **Engine layer (Go, inside `preview_spec`):** the EXISTING curator
  render + hardened Judge, on the curator config's paid model
  (Anthropic Opus). This is the only premium spend and it is
  per-explicit-preview, agent cannot loop it unbounded (gate + §5 D4).

## 4. Test strategy (PATTERN §3 + curator four-level pyramid)

- Node: `registerFauxProvider` scripted agent turns ($0, offline,
  deterministic) for unit→scenario; thin live 0x smoke only.
- Go `curator-api`: unit (handler) → integration (real packages,
  `replay`/`none` LLM provider) → contract (the Node↔Go JSON shapes,
  golden) → scenario (full chat→edit→preview→apply against a temp
  brain + temp specs dir). The Judge already has its pyramid; reuse.

## 5. OPEN DECISIONS — to close conversationally before any code

**D1 — boundary transport.** Resolved-by-pattern: localhost HTTP/JSON
Go `curator-api` (NOT shell-scrape, NOT CGO). Confirm framing only.

**D2 — `.doc.yaml` write fidelity.** RESOLVED 2026-05-17 (user):
**(a) `yaml.Node` structured edit** — parse the hand-authored file to
the yaml.v3 node tree, mutate only the touched nodes, re-emit;
comments / key-order / untouched formatting preserved. Rejected: (b)
struct→`yaml.Marshal` (drops comments, reorders keys, every edit
reformats the whole file → noisy diffs, lost authored guidance); (c)
diff-only (breaks the interactive apply-loop; its safety is already
provided by the D2-gate). Apply gate = propose-diff → explicit human
ACK → write (same hard-HITL posture as D6; never autosave).

**D3 — preview composite vs. split.** RESOLVED 2026-05-17 (reasoned):
**composite** `preview_spec` — render→judge→diff in one call. The
agent structurally cannot propose a `put_doc_spec`/`propose_kb_entry`
without the hardened Judge having scored the candidate; split tools
let a turn skip Judge and lose the loop's whole point. Split buys
flexibility v1 doesn't need.

**D4 — premium-spend bound.** RESOLVED 2026-05-17 (user): enforced in
`beforeToolCall` — (1) **per-turn single `preview_spec`** (no
autonomous render→tweak→render loop within one user message); (2)
**per-session preview ceiling**, default **10 / session,
configurable**, `curator-api` returns engine `usage.cost`,
agent-service accumulates, gate blocks past ceiling, remaining budget
surfaced in chat. Explicit-intent gating REJECTED (throttles the core
loop; the propose→ACK gate already bounds the expensive *outcome*).
NB: the standing prompt-eng-autonomy memory authorizes the
*operator's* deliberate scoped batch re-verify — NOT an LLM looping
Opus inside a user chat; this agent carries its own bound.

**D7 — pi-web-ui integration shape.** RESOLVED 2026-05-17 (user):
spike-04 (E4/KU4, live-Playwright) verified pi-web-ui's NATIVE path
runs the agent-core `Agent` IN THE BROWSER with browser-side API-key
auth (IndexedDB) — contradicts D1 (creds server-side). Chosen: **(a)
pi-web-ui ChatPanel + a server-delegating transport shim** — the
browser mounts ChatPanel over an `Agent` whose LLM transport
delegates every turn to the server-side `/chat` (the real
Agent+gate+OAuth+tools stay server-side; the browser holds no creds,
the browser Agent is a UI-state shell). Honors PATTERN adopt-don't-
build AND D1. Rejected: (b) presentational-only (drifts to building),
(c) defer. v1 SCOPE NOTE: server runs tools, so the browser Agent
emits no tool-exec events → pi-web-ui's rich tool-render is not wired
in v1; conversation + a custom approval-confirm panel (drives
`/approve`) is. SSE-bridging server tool events into pi-web-ui
rendering is a v2 enhancement. Apply the verified GOTCHA: repoint
`app.css` @import to `../node_modules/@earendil-works/pi-web-ui/dist/
app.css`; build on node ≥20.19 (the pi image is node:22 — build
there, never host).

**D5 — v1 scope cut.** RESOLVED 2026-05-17 (user): single page;
`read_kb_area` + `get/put_doc_spec` (incl. widen-sources) + gated
`propose_kb_entry` + `preview_spec` + gated apply. v1 **does** close
brain-content gaps (GRS class), not just spec defects. Still v2:
multi-spec/cluster chat; repoint Go `llm.Client` `"pi"` at this
service; entry *update/verify/promote* (v1 is add-only, `incoming`).

**D6 — `propose_kb_entry` persistence + brain-write safety.** This is
the first agent that mutates the REAL `~/.mykb`. Open:
(i) **Write path:** shell the sanctioned `kb add <type>` CLI (the
brain's transactional boundary — rebuilds `kb.db`, updates
`manifest.json`, git) vs. direct JSONL append (fast, but bypasses
index/manifest/git — exactly what the `kb` vs `kb-develop` split
exists to prevent; rejected unless argued).
(ii) **Which binary:** `kb` (stable) per CLAUDE.md operator-activity
rule — never `kb-develop` against the real brain.
(iii) **Provenance:** `--source` MANDATORY (verification-first
protocol); entry lands in `incoming`/unverified zone; a human must
`kb verify` later — the agent may never self-verify.
(iv) **Gate:** propose-diff → explicit human ACK → write; never
autosave; the brain write is a hard-HITL mutation (same posture as
spec apply, higher stakes).

## 6. Live-UX smoke (repeatable)

Deterministic, $0, no OAuth — the real built SPA + real static
server + real RemoteAgent transport, Agent/LLM stubbed:

```sh
cd agent-service
docker build -t mykb-curator-agent-service:dev .
docker run --rm -v "$PWD/scripts:/pi/app/scripts:ro" \
  -e HOST=0.0.0.0 -e WEB_ROOT=/pi/app/web/dist -e PORT=4774 \
  -p 127.0.0.1:4778:4774 mykb-curator-agent-service:dev \
  sh -c 'cd /pi/app && node scripts/smoke-web.mjs'
# then drive http://127.0.0.1:4778 in a browser (or Playwright):
# type a "widen the sources" prompt -> the D2/D6 "Approval
# required" panel appears -> Approve & apply -> auto re-prompt ->
# "Applied". Verified live 2026-05-17 (status header).
```
