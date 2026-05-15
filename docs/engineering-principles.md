# Engineering Principles — mykb-curator

**Status:** ADOPTED v1.0 — non-negotiable for all work in this repo from day 1.

**Scope:** Every PR, every spec, every implementation in `vilosource/mykb-curator`.

**Upstream:** This document **derives from and is downstream of** the ViloForge canonical north star (`viloforge-platform/docs/engineering-principles.md`). Where the canonical and this document agree, the canonical is authoritative. Where this document concretizes for `mykb-curator` specifically (TypeScript/Node tooling, the curator's component taxonomy, the solo-dev workflow), this document is authoritative for this repo.

**Owner:** lonvilo@pm.me

---

## 1. Purpose

This document is the north star for how `mykb-curator` is designed and built. It exists for two reasons:

1. **Consistent quality across contributors.** Whether work is run by the maintainer interactively, an autonomous agent, or a future external contributor, the standard is the same. Ad-hoc standards produce uneven quality.
2. **Methodology propagation.** Every PR template, every test plan, every code review derives from this document. The principles are not aspirational. **Violations are merge blockers, not "we can fix it later".**

---

## 2. Design principles

### 2.1 SOLID — concretized for `mykb-curator`

The five SOLID principles, with operational meanings specific to the curator's component taxonomy from [`DESIGN.md`](DESIGN.md).

**S — Single Responsibility Principle.**

Each module has one reason to change.

| Component | Single responsibility |
|---|---|
| `KBSourceAdapter` impls | Fetch kb snapshot. Not parse it. Not cache it. |
| `SpecStoreAdapter` impls | Pull specs from a store. Not validate them. |
| `WikiTargetAdapter` impls | Speak the wiki backend's API. Not reconcile. Not render. |
| `Frontend` impls | Spec + kb → IR. Not validate IR. Not render. |
| `Pass` impls | IR → IR with one declared concern (e.g., "resolve kb-refs"). One pass, one transformation. |
| `Backend` impls | IR → target-format string. No I/O. |
| `Reconciler` | Apply two-zone rules to (current wiki state, new render). Not push to wiki. |
| `CacheManager` | Key/value persistence + hash composition. Not decide what to cache. |
| `LLMClient` | HTTPS + retry + cache + cost accounting. Not decide which prompts to send. |
| `Reporter` | Assemble + serialize the run report. Not decide what gets reported. |

Operational test: *"If I changed the policy for X, would this code also need to change?"* If a single class would change for both "we switched LLM providers" AND "we changed which prompts we use," SRP is violated — split it.

Anti-pattern to avoid: a `Curator` god class that orchestrates AND renders AND reconciles AND reports. The `Orchestrator` orchestrates only; everything else is its collaborators.

**O — Open/Closed Principle.**

The curator MUST be addable-to without modifying the dispatcher/registry layers.

| Addable thing | OCP demand |
|---|---|
| New `Frontend` (e.g., `RunbookFrontend`) | Register in the frontend registry; orchestrator code does not change. |
| New `Pass` (e.g., `EnforceTerminology`) | Add to the pass pipeline config; pipeline-runner code does not change. |
| New `Backend` (e.g., `ConfluenceBackend`) | Implement the backend interface; backend registry picks it up. |
| New `Check` (e.g., `SystemRealityProbe`) | Register in the maintenance pipeline's check registry. |
| New `KBSourceAdapter` (e.g., `V2DaemonAdapter`) | Implement the adapter interface; config selects it by type. |
| New diagram renderer (e.g., `PlantUMLRenderer`) | Register with the `RenderDiagrams` pass's renderer registry. |
| New wiki target | Implement `WikiTarget` interface. |

Operational test: *"Can a future contributor add the next $THING without modifying existing $THING code?"* If they'd have to touch a switch statement, type check, or hardcoded list — that's an OCP violation. Refactor to a registry before merging.

**L — Liskov Substitution Principle.**

Subtypes substitute for base types without surprising consumers.

- Every `WikiTarget` impl obeys the same contract: `getPage`, `upsertPage`, `humanEditsSinceBot`, etc. Tests against the `Reconciler` pass when a `FakeWikiTarget`, a `MediaWikiTarget`, or a future `ConfluenceTarget` is injected.
- Every `Pass` impl obeys: pure function `IR → IR`, no I/O except declared logging, idempotent on its own output.
- Every `Backend` impl obeys: pure function `(IR, backendConfig) → string`, no I/O, deterministic.

LSP violation surfaces as: *"the test passes against the real impl but fails against the test double"* — a sign the double isn't a real Liskov substitute, OR the real impl is doing something off-contract.

**I — Interface Segregation Principle.**

Many small client-specific interfaces over one fat one.

- A consumer that only reads kb gets a `KBReader` interface. A consumer that only writes (e.g., the maintenance pipeline opening PRs) gets a `KBWriter` interface. Not one `KBClient` with 40 methods.
- The `WikiTarget` is intentionally a single interface because all curator code paths need most of it — but if a future consumer only needs to *read* wikis (e.g., a separate audit tool), split into `WikiReader` + `WikiWriter`.
- The CLI doesn't import the orchestrator's internals; it imports a small `Runner` facade.

ISP violation: any module imports a >10-method interface to use 2 of them.

**D — Dependency Inversion Principle.**

High-level code does not know about low-level details.

- The `Orchestrator` depends on `KBSourceAdapter` (interface), not `GitKBSource` (impl). Tests inject `InMemoryKBSource`.
- The `Reconciler` depends on `WikiTarget` (interface), not `MediaWikiTarget`.
- The `EditorialFrontend` depends on `LLMClient` (interface), not `AnthropicSDK`. Tests inject `ReplayLLMClient` that reads cached fixture responses.
- The pass pipeline depends on a `PassRegistry` interface, not on the concrete pass list.

DIP violation smell: any high-level module has a `new GitKBSource(...)` or `new MediaWikiTarget(...)` outside of the composition root (the entrypoint that wires concretions to abstractions). Composition happens at the edges; business logic stays abstract.

### 2.2 Extensibility — v1 contracts MUST already include affordances for v2+

For each declared future feature in [`DESIGN.md §17 Roadmap`](DESIGN.md#17-roadmap), the v1 contract MUST already include the affordance so the future addition is non-breaking.

| Future feature (v2+) | v1 affordance |
|---|---|
| **Confluence/Notion/static-HTML backends** | `Backend` interface is stable; backend registry is the only dispatch; backends are registered by name; IR contains no MediaWiki-specific concepts. |
| **v2-daemon as kb source** | `KBSourceAdapter` interface stable; `kb_source.type` in config enables selection; the orchestrator never assumes git operations on the kb. |
| **Pre-filtered kb adapter** (tag-based cross-tenant defense) | `KBSourceAdapter` interface includes a `filter` parameter from v1; v1 git adapter accepts it and ignores it (or honors a simple area-include list). |
| **Multi-brain runs** | Config schema's `kb_source` slot is a list (or single-which-extends-to-list) from v1, even if v1 only handles one. |
| **Per-block 3-way merge in reconciler** | Reconciler emits a `ReconcileEvent` stream with full context (before/after/human-edit), so a future merger can attach as a consumer. v1 implements overwrite-with-report; v2 attaches a merger. |
| **Style rules as plugins** | `ApplyStyleRules` pass takes a rules registry from v1; v1 ships 2-3 hardcoded rules in the registry. v2 reads from spec-store. |
| **Multiple diagram renderers** (PlantUML, draw.io) | `RenderDiagrams` pass has a renderer registry; v1 registers only `MermaidRenderer`; renderers selected by diagram-block's `lang` field. |
| **External truth check (web search)** | Maintenance pipeline's `Check` interface accepts a `WebSearcher` dep from v1, even if v1 has no checks that use it. |
| **Conversational `spec init` UX** | Spec schema is versioned (`version: 1` in frontmatter); spec-engine validates schema version; future `spec init` lives in CLI cleanly separated from core. |
| **Multi-tenant SaaS** (curator-as-a-service, far future) | Every run-report and cache key already includes `wiki:` tenant id; nothing in core is hardcoded single-tenant. |

**Principle:** for every deferred feature, write the hypothetical migration path during design review. If it requires breaking any v1 contract, the v1 contract is wrong — fix v1 before merging.

### 2.3 Design patterns — actively used, not cataloged

These patterns are deliberately applied in `mykb-curator`. Spec/PR descriptions must call out which pattern is being used and why.

| Pattern | Where in the curator | Why this pattern |
|---|---|---|
| **Strategy** | `KBSourceAdapter`, `SpecStoreAdapter`, `WikiTargetAdapter`, `Frontend`, `Backend`, diagram renderers, maintenance `Check` | Multiple interchangeable implementations of the same contract; runtime selection by config. |
| **Chain of Responsibility / Pipeline** | The Pass pipeline (`IR → IR → IR → ...`) | Composable, reorderable, individually testable transformations. |
| **Factory** | IR block constructors (`makeProseBlock`, `makeMachineBlock`, ...); frontend/backend/check registries | Centralised construction with validation; consumers don't `new` blocks directly. |
| **Repository** | `SpecStore` (abstracted over storage backend), `KBReader` (abstracted over kb format) | Storage backend can change (git → s3 → daemon) without changing consumers. |
| **Specification** | Spec `include:` filters as composable kb-query specifications (area, workspace, zone, tag) | Filters are data; can be combined, negated, audited; future tag-based defense-in-depth slots in naturally. |
| **Decorator** | `LLMClient` wrapped with cache → rate-limit → retry → telemetry layers | Cross-cutting concerns added without polluting the core client. |
| **Adapter** | Three pluggable backends (KB, Spec, Wiki) are all Adapters between an external API and the curator's internal interfaces | Isolates external API churn; same pattern across all three lets contributors learn one shape. |
| **Observer / Pub-Sub** | `RunReporter` subscribes to pipeline-stage events emitted by the `Orchestrator` and `Passes` | Reporter doesn't pollute pipeline code; multiple sinks (file, Slack, journal) all subscribe. |
| **Command** | CLI subcommands (`run`, `spec init`, `reconcile`, `report`) are explicit Command objects | Each command is testable in isolation; new commands add by registering, not by editing dispatcher. |
| **Builder** | IR `PageDocBuilder` for fluent construction in frontends | Frontends build IR step-by-step; builder enforces invariants. |
| **Template Method** | Base `Pass` class defines the lifecycle (validate input → transform → validate output → emit events); concrete passes fill in the transform | Lifecycle invariants enforced centrally; pass authors focus on their concern. |

**This is not a checklist.** Using all patterns in one PR is over-engineering. Use the pattern that fits; explain why; reject patterns that don't fit. Pattern names in PRs/specs are *communication*, not theatre.

---

## 3. Development principles

### 3.1 TDD — red / green / refactor

**Every implementation follows test-driven development.**

The discipline:

1. **Red.** Write a failing test that captures the desired behavior. Run it. See it fail with a meaningful error.
2. **Green.** Write the minimum code to make the test pass. No gold-plating. Run again. See it pass.
3. **Refactor.** Improve the code without changing observable behavior. Tests still pass.

Operational conventions for this repo:

- Every public function/method/class has at least one test written *before* the implementation.
- Test names document expected behavior: `describe('MediaWikiBackend.render', () => { it('renders a TableBlock as a wikitable', ...) })`.
- Unit tests run in **< 100ms each**. If not, refactor for testability — usually means a missing seam (no DI, hidden I/O, time/randomness coupling).
- A code-bearing PR with zero new tests is a methodology violation. The merge bar requires tests at the appropriate pyramid level (see §3.2).
- "Refactor while green" is a deliberate, named phase — separate commits from green-making commits where practical.

**Why TDD specifically:**

1. **Tests are the executable specification.** A test passing proves the behavior holds; the test name documents what behavior. Skip-and-document and you lose both proofs.
2. **TDD enforces SOLID by friction.** If a class is hard to unit-test, it's coupled too tightly. The pain of writing the test surfaces the SOLID violation before the implementation freezes it in.
3. **TDD compounds.** Each passing test is a regression check forever. After 50 PRs the suite catches what code review can't.

### 3.2 Testing pyramid — up to scenario tests

Four levels, ordered bottom-up. **All four are mandatory** at the levels each change touches.

#### Level 1 — Unit tests (most numerous, fastest)

**What:** Per-function / per-class behaviour with dependencies mocked.

**For `mykb-curator` specifically — examples of what gets unit-tested:**

| Component | Unit test |
|---|---|
| Each `Pass` | Fixture IR in, expected IR out. Tests the transformation in isolation. |
| Each `Backend` | Fixture IR in, expected target-format string out. Pure function. |
| Each `Frontend` (deterministic parts) | Fixture (spec, kb-subset) in, expected IR structure out. LLM-mocked for `EditorialFrontend`. |
| `Reconciler` two-zone logic | Fixture (current page, new render, history) in, expected (next-state, events) out. |
| `CacheManager` hash composition | (spec_hash, kb_hash, pipeline_version) → cache_key; verify stability and uniqueness. |
| `SpecEngine` parsing | Fixture spec markdown in, expected parsed-spec out; invalid specs raise specific errors. |
| `KBSourceAdapter` impls | With an `InMemoryGit` fixture, verify clone/pull behavior. |
| `WikiTargetAdapter` impls | Against an `mwn` mock, verify API call sequences. |
| `LLMClient` decorators | Each decorator (cache / retry / rate-limit) tested in isolation against a stub inner client. |
| IR `PageDocBuilder` | Construction sequences produce expected IR; invalid sequences error. |

**Speed:** < 100ms each. Total unit suite < 30s.

**Run on:** every save (watch mode) + pre-commit + every PR.

**Lives in:** `tests/unit/`.

**Vitest config:** default suite; no special pool.

#### Level 2 — Integration tests (fewer, slower)

**What:** Real components talking to real components (not mocks) — but bounded to in-process or local containers.

**For `mykb-curator` specifically — examples:**

| Integration test | What's real |
|---|---|
| Full Page Rendering pipeline against a fixture kb | Real frontend (deterministic) + real passes + real backend; fake LLM (replay); in-memory wiki target |
| `GitKBSource` against a real local git repo | Spin up a temp git repo with fixture commits; verify diff-since-commit returns expected areas |
| `MediaWikiBackend` against a real MediaWiki container | docker-compose'd test MediaWiki; render a fixture IR; verify wikitext renders correctly when posted |
| Cache round-trip | Real filesystem; write/read/invalidate IR cache; verify hash-keyed retrieval |
| `Reconciler` end-to-end on a real test MediaWiki page | Render → push → re-render with no kb change → verify no-op; then simulate human edit + verify detection |
| `SpecStore` Git adapter against a real git repo | Real clone, real pull, real validation |
| KB Maintenance pipeline opening a PR against a real git forge | Use a local Gitea or a throwaway GitHub repo; verify branch + PR creation |

**Speed:** seconds each. Total integration suite < 5 minutes.

**Run on:** every PR commit (CI).

**Lives in:** `tests/integration/`.

**Vitest config:** separate test suite with `--testPathPattern=tests/integration`; docker-compose fixtures spun up per suite.

#### Level 3 — Contract tests (between components or against external APIs)

**What:** Verify that contracts at the boundaries hold under version evolution.

**For `mykb-curator` specifically — examples:**

| Contract test | What's pinned |
|---|---|
| **Spec schema contract** | `tests/contract/spec-schema.test.ts` validates every fixture spec in `tests/fixtures/specs/` against the current schema; breaking the schema = mass test failure. |
| **IR schema contract** | `tests/contract/ir-schema.test.ts` validates IR JSON-dumps; backends consuming IR are tested against the schema so backend-vs-pipeline drift surfaces. |
| **MediaWiki API contract** | Replay-based tests: recorded MediaWiki API responses (per supported version) are replayed against the adapter; if MediaWiki changes its response shape, the recording must be updated and a version-compatibility note added. |
| **LLM prompt-response contract** | Fixture prompts → fixture responses (cached). Replay against `ReplayLLMClient`. If frontend changes prompts, recordings invalidate and must be regenerated with a real model run + reviewed. |
| **Wiki backend contract** | Every `WikiTarget` impl runs against the same contract test suite (`WikiTargetContractSuite`); ensures all backends are LSP-substitutable. |
| **KB-source contract** | Same — every `KBSourceAdapter` runs against `KBSourceContractSuite`. |
| **Cache key stability** | Pin cache-key generation; if pipeline version bumps without explicit `pipeline_version` increment, fail. Prevents silent cache pollution. |

**Speed:** seconds each. Total contract suite < 3 minutes.

**Run on:** every PR.

**Lives in:** `tests/contract/`.

**Vitest config:** separate suite; contract test runner enumerates all impls of each interface and runs them through a shared spec.

#### Level 4 — Scenario / end-to-end tests (fewest, slowest)

**What:** Full user flows through the whole system, real services, real network.

**For `mykb-curator` specifically — examples:**

| Scenario | What it exercises |
|---|---|
| **First render of a projection page** | Configure a test wiki + test kb; run `mykb-curator run --wiki test`; verify page exists with expected structure. |
| **Second render no-op** | Re-run without kb changes; verify zero wiki revisions created. |
| **Diff-driven re-render** | Add a fact to the kb; run; verify only the affected page re-renders. |
| **Human edit reconciliation — machine zone** | Manually edit content inside `CURATOR:BEGIN/END` markers; run; verify edit overwritten and run report flags it. |
| **Human edit reconciliation — editorial zone** | Manually polish a prose paragraph; run with unchanged inputs; verify polish preserved. |
| **Editorial mode end-to-end** | Configure an editorial spec; run; verify LLM frontend produced IR; verify final page has sections matching spec; verify second run uses cache. |
| **KB maintenance PR flow** | Stale fact in kb + external truth check opted in; run; verify a PR opens against the test kb repo with the proposed verification. |
| **Wiki API outage** | Block wiki API mid-run; verify graceful failure; verify run report records the failure; verify retry on next run succeeds. |
| **Concurrent run** | Start two `mykb-curator run --wiki test` processes; verify second exits cleanly with "wiki locked." |
| **Spec validation failure** | Submit a malformed spec; verify it's skipped, others process, run report names it. |
| **Multi-wiki run** | Configure two wikis; run `--all`; verify both processed without cross-contamination. |

**Speed:** minutes each. Total scenario suite < 30 minutes.

**Run on:** every release candidate + nightly CI.

**Lives in:** `tests/scenario/`.

**Infrastructure:** docker-compose with test MediaWiki + test git server + LLM replay; `bats` for shell-level orchestration of multi-process scenarios; vitest for in-process scenarios.

#### Pyramid invariants

- **Each level depends only on tooling at its level or below.** Unit tests never spin up containers; integration tests never make real LLM API calls (they replay).
- **Higher-level test → lower-level tests required.** A new scenario test for "human edit reconciliation" must be accompanied by unit tests on the `Reconciler` logic and integration tests on the wiki adapter. You don't write only a scenario test for new code.
- **Test counts roughly follow the pyramid:** many unit, fewer integration, fewer still contract, fewest scenario.
- **Speed follows the inverse:** fastest at the bottom.

---

## 4. How design + development principles reinforce each other

- **SOLID makes TDD possible.** A tightly-coupled `Curator` god class can't be unit-tested in isolation; you'd mock the universe. Each component being SRP + DIP means each has a small surface and clear seams.
- **TDD enforces SOLID.** Writing the test first forces *"what's the smallest surface I can drive this through?"* — that's interface design. Painful tests = SOLID-violation signal.
- **Patterns are tested at the right level.**
  - **Strategy** impls → unit-test each strategy in isolation + contract-test that all impls satisfy the shared contract.
  - **Pipeline** → unit-test each pass; integration-test the assembled pipeline against fixture IR.
  - **Repository** → unit-test the abstraction; integration-test the impl against real storage.
  - **Pub-Sub** → contract-test the event schema; scenario-test that subscribers receive expected events.
- **Extensibility is verified by extending.** To prove the backend registry is open for extension: in v2, the contributor adds `ConfluenceBackend`, registers it, and zero existing code changes. If existing tests all still pass and a new contract-test for `ConfluenceBackend` is added, OCP held.

---

## 5. Application checklist by role

Since this is a solo-dev / small-contributor project (not the ViloForge SDD multi-agent pipeline), the role split is simpler.

### 5.1 Designer (the person writing a DESIGN.md or significant spec)

- Declare which design patterns the design uses and why (in the design body, not separately).
- For each declared future feature in the roadmap, document the affordance the v1 architecture provides.
- For each new component, name which SOLID principles are load-bearing (e.g., "this introduces a new backend; OCP must hold via the registry").

### 5.2 Implementer (the person opening a PR)

- Follow TDD red/green for every implementation step. Test commits separate from impl commits where practical.
- Tests at every applicable pyramid level — unit (always for new code) + integration (when crossing component boundaries) + contract (when extending a Strategy contract) + scenario (when adding a new end-to-end user flow).
- Run the full local suite (unit + integration + contract) before opening the PR. Red tests = no merge.
- Use the design patterns the design calls for. If a different pattern fits better, raise it in the PR description — don't silently substitute.

### 5.3 Reviewer (operator or future contributor reviewing PRs)

- Verify test coverage at applicable pyramid levels. Missing tests at any required level = blocker.
- Verify pattern claims match the implementation (no "Strategy" claim covering a switch statement).
- Verify SOLID-violation absence by reading the diff: god classes, hardcoded concretions outside the composition root, fat interfaces.
- Verify extensibility affordances stay intact (no v1 contract narrowing that breaks roadmap items).

---

## 6. Enforcement — where principles are checked

Multi-layer defense in depth:

1. **PR template** — every PR must check off: tests added at applicable levels, design patterns named, SOLID concerns considered.
2. **Code review** — operator/reviewer checks per §5.3.
3. **CI (when wired)** — automated enforcement:
   - Unit + integration + contract tests on every PR
   - Scenario tests on every release candidate + nightly
   - Type check (TypeScript strict mode) on every PR
   - Lint (ESLint with project rules) on every PR
4. **Release gate** — no release ships with red tests at any pyramid level.

Each layer is necessary but not sufficient. A PR passing CI but violating SOLID surfaces in code review. A PR passing review but missing scenario tests fails the release gate.

---

## 7. Non-negotiability — when can principles be skipped?

The principles are mandatory. The few cases where they can be relaxed:

- **Trivial PRs** (typo fix, dep bump, doc-only change with no executable claims): TDD red/green skipped. Pattern/SOLID still apply if any code touches.
- **Explicit operator override** in PR description: if a specific PR doesn't need a specific principle, document the override with a rationale. Tracked; future retros may revisit.
- **Exploratory spikes** under `spikes/` *not intended to ship*: TDD may be skipped. The spike's findings produce a real PR that follows the full discipline.

That's the complete list. **There is no "we're in a hurry" or "the test is hard to write" exception. If a test is hard to write, the code is wrong.**

---

## 8. Relationship to other docs

- **[`docs/DESIGN.md`](DESIGN.md)** — architecture; this doc enforces how that architecture gets built.
- **`viloforge-platform/docs/engineering-principles.md`** — upstream canonical; this doc derives from it. When the canonical updates with general improvements, port them here.
- **Future `CONTRIBUTING.md`** — will reference this doc as the bar for contributions.
- **Future `tests/README.md`** — will document the test-runner conventions, fixture layout, and how to add tests at each pyramid level.

---

## 9. Tooling (this repo specifically)

| Concern | Tool |
|---|---|
| Language | TypeScript (strict mode) |
| Runtime | Node.js (current LTS) |
| Unit + Integration + Contract tests | [vitest](https://vitest.dev) |
| Scenario tests | vitest (in-process) + [bats](https://bats-core.readthedocs.io/) (multi-process / shell) |
| Coverage | vitest's built-in (c8) |
| Lint | ESLint + `@typescript-eslint` |
| Format | Prettier |
| Type check | `tsc --noEmit` in CI |
| Test containers | docker-compose for MediaWiki, git server fixtures |
| LLM fixtures | Recorded responses keyed by `(prompt_hash, model_id)`, committed to `tests/fixtures/llm/` |

Vitest test markers / suites:

```ts
// tests/unit/passes/resolve-kbrefs.test.ts
describe('ResolveKBRefs', () => { ... });

// tests/integration/rendering-pipeline.test.ts
describe('Rendering pipeline against fixture kb', () => { ... });

// tests/contract/wiki-target.contract.test.ts
describe.each(allWikiTargetImpls)('WikiTarget contract — %s', (impl) => { ... });

// tests/scenario/first-render.scenario.test.ts
describe('Scenario: first render of a projection page', () => { ... });
```

---

## 10. References

- ViloForge canonical: `viloforge-platform/docs/engineering-principles.md` (this doc derives from it)
- [`docs/DESIGN.md`](DESIGN.md) — the architecture these principles enforce against
- [SOLID — Robert C. Martin's foundational article](https://web.archive.org/web/20150906155800/http://www.objectmentor.com/resources/articles/Principles_and_Patterns.pdf)
- [Test Pyramid — Mike Cohn, popularised by Martin Fowler](https://martinfowler.com/articles/practical-test-pyramid.html)
- [TDD — Kent Beck](https://www.kent-beck.com/tdd)
