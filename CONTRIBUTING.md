# Contributing to mykb-curator

## Ground rules (non-negotiable)

This repo follows the ViloForge engineering north star, concretised
in [`docs/engineering-principles.md`](docs/engineering-principles.md).
Read it. The short version:

1. **`docs/DESIGN.md` is the source of truth.** Behaviour-bearing
   changes update it in the same PR. §17 is the live roadmap/status
   table — keep it honest as work lands.
2. **Strict TDD, red → green, every slice.** Write the failing Go
   test first, watch it fail, then write the minimal code to pass,
   then refactor. Never code-first.
3. **Four-level testing pyramid, no level skipped.** The `Makefile`
   *is* the pyramid (see below). Every behaviour-bearing change gets
   coverage at every level it implicates.
4. **SOLID + extensibility.** The compiler shape (frontend → IR →
   passes → backend) and the adapter Strategies exist for this. A new
   backend/frontend/pass/check/sink is a **new implementation**, not
   an edit to the pipeline. Boundaries (LLM, wiki, web search,
   mail, exec) are injected interfaces so units are deterministic and
   network-free.
5. **Faithful reporting.** Never fake a test/scenario run. Name
   deferrals with a reason and a closing condition (see the mmdc /
   web-search-provider deferrals for the expected shape).

## The pyramid (use the Makefile)

| Command | Level | What |
|---|---|---|
| `make check` | gate | `fmt` + `vet` + `lint` + `test-unit` — the default pre-commit gate |
| `make test-unit` | L1 | plain Go unit tests |
| `make test-integration` | L2 | `//go:build integration` — cross-component, real local adapters |
| `make test-contract` | L3 | `//go:build contract` — adapter contract suites (`Backend`, `Frontend`, `wiki.Target`, `kb.Source`) |
| `make test-scenario` | L4 | `//go:build scenario` — real MediaWiki via testcontainers-go (**Docker required**) |
| `make test-all` | all | all four; run before anything you call "done" |

A new `wiki.Target` / `kb.Source` / backend / frontend must satisfy
its **L3 contract suite** — register the impl in the suite, don't
duplicate assertions.

## Per-slice workflow

1. Failing test first (`make test-unit` shows red).
2. Minimal code to green; refactor.
3. Add coverage at every pyramid level the change implicates.
4. `make check` green.
5. Commit (see message rules), push.
6. **CI green before the next slice.** Pre-existing flakes are
   blockers unless explicitly tracked + waived.
7. `make test-all` before declaring the slice done.

## Toolchain

- **Go 1.25** (`go.mod` targets `go 1.25`; a dep auto-bumped it —
  don't lower it). CI runs Go 1.25.
- **golangci-lint v2** (the `.golangci.yml` is v2 format). A v1
  binary fails with a config-version error. Install v2:
  ```
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh \
    | sh -s -- -b $(go env GOPATH)/bin
  ```
  CI uses `golangci-lint-action@v7` (v2.4+, Go-1.25-built).
- **Docker** for L4 scenario tests (testcontainers-go).
- Public-registry deps: if your environment's npm/Go proxy points at
  a private mirror, fetch deps from the public proxy so `go.sum`
  isn't tainted. `go mod tidy` must leave `go.mod`/`go.sum` clean
  (CI enforces a tidy check).

## Commit & PR rules

- Conventional-style subject: `feat(pkg): …`, `fix(pkg): …`,
  `docs: …`, `test(pkg): …`.
- **No AI-attribution trailers** (`Co-Authored-By: Claude/…`,
  "Generated with …"). Record AI involvement in the PR description
  instead, not in commit metadata.
- Body: what changed and why, the pyramid levels exercised, and any
  named deferral with its closing condition.
- One logical slice per commit; `make check` green before each.

## Adding things (the extension points)

- **A rendering pass** → new package under
  `internal/pipelines/rendering/passes/<name>`, implement
  `passes.Pass`, wire it into `composePassPipeline` in the
  composition root in DESIGN §5.4 order.
- **A maintenance check** → new package under
  `internal/pipelines/maintenance/checks/<name>`, implement
  `maintenance.Check`, wire in `composeMaintenance`. Expensive
  (LLM/web) checks must respect the §6.4 funding gate (opt-in via
  spec `fact_check`).
- **A wiki/kb/backend/frontend adapter** → implement the interface,
  register in the L3 contract suite.
- **A report sink** → implement `reporter.Sink` in
  `internal/reporter/sinks`, take the external boundary as an
  injected interface, wire in `composeReportSinks`.

## Writing specs

Authoring page specs (not code) → see
[`docs/spec-authoring-guide.md`](docs/spec-authoring-guide.md).
